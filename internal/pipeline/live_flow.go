package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/prefilter"
	"time-series-rag-agent/internal/storage/postgresql"
	"time-series-rag-agent/internal/trade"
	pkg "time-series-rag-agent/pkg/notifier"

	"github.com/adshao/go-binance/v2/futures"
	"golang.org/x/sync/errgroup"
)

func NewLivePipeline(ctx context.Context, logger *slog.Logger, binanceClient *futures.Client, hooks *pkg.PipelineHooks, wsCandle []exchange.WsCandle, symbol string, interval string, vectorSize int, wsClose float64) error {
	logger.Info("[LivePipeline] Starting Embedding Pipeline")
	cfg := config.LoadConfig()
	adapter := exchange.NewBinanceAdapter(binanceClient)

	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser, cfg.Database.DBPassword,
		cfg.Database.DBHost, cfg.Database.DBPort, cfg.Database.DBName,
	)

	duration, err := parseBinanceInterval(interval)
	if err != nil {
		return fmt.Errorf("[LivePipeline] parse interval: %w", err)
	}

	executor := exchange.NewExecutor(
		binanceClient,
		symbol,
		cfg.Agent.AviableTradeRatio,
		cfg.Agent.Leverage,
		cfg.Agent.SLPercentage,
		cfg.Agent.TPPercentage,
		*logger,
	)

	// --- 1) REST fetch + DB connect + Cooldown check in parallel (fail-fast) ---
	var (
		restCandle    []exchange.RestCandle
		dbIngest      *postgresql.PatternStore
		isInCooldown  bool
		barsRemaining int
	)

	g1, ctx1 := errgroup.WithContext(ctx)

	g1.Go(func() error {
		var err error
		restCandle, err = exchange.FetchLatestCandles(ctx1, adapter, symbol, interval, vectorSize+1+99)
		return err
	})

	g1.Go(func() error {
		var err error
		dbIngest, err = postgresql.NewPostgresDB(ctx1, connString, *logger)
		return err
	})

	g1.Go(func() error {
		var err error
		isInCooldown, barsRemaining, err = executor.GetCooldownState(ctx1, duration)
		return err
	})

	if err := g1.Wait(); err != nil {
		if dbIngest != nil {
			dbIngest.Close()
		}
		hooks.OnPipelineError("init", err)
		return fmt.Errorf("[LivePipeline] init: %w", err)
	}
	defer dbIngest.Close()

	// --- 2) Embedding (sequential, depends on restCandle + dbIngest) ---
	feature, label, wsRestCandle := NewEmbeddingPipeline(*logger, wsCandle, restCandle, vectorSize, symbol, interval)
	if feature == nil {
		hooks.OnPipelineError("embedding", fmt.Errorf("feature is nil"))
		return fmt.Errorf("[LivePipeline] feature is nil")
	}

	logger.Info("[LivePipeline] feature time", "unix", feature.Time.Unix(), "ws_time", wsCandle[len(wsCandle)-1].Time)

	// --- 3) DB upserts (ทำเสมอ ไม่ว่าจะ cooldown หรือไม่) ---
	g2, ctx2 := errgroup.WithContext(ctx)

	g2.Go(func() error {
		if err := dbIngest.UpsertFeature(ctx2, *feature); err != nil {
			return fmt.Errorf("upsert feature: %w", err)
		}
		logger.Info("[LivePipeline] Ingested feature")
		return nil
	})

	g2.Go(func() error {
		if err := dbIngest.UpsertLabels(ctx2, symbol, interval, label); err != nil {
			return fmt.Errorf("upsert labels: %w", err)
		}
		logger.Info("[LivePipeline] Ingested label")
		return nil
	})

	// TODO running only at 00 minute porint of time
	g2.Go(func() error {
		if err := RestIngestVectorFlow(logger, symbol, "1h", vectorSize); err != nil {
			return fmt.Errorf("ingest 1h timeframe: %w", err)
		}
		logger.Info("[LivePipeline] Ingested 1 hour timeframe")
		return nil
	})

	if err := g2.Wait(); err != nil {
		hooks.OnPipelineError("phase2", err)
		return fmt.Errorf("[LivePipeline] phase 2: %w", err)
	}

	hasPosition, side, _, err := executor.HasOpenPosition(ctx)
	if err != nil {
		return fmt.Errorf("[LivePipeline] Checking position error: %w", err)
	}
	if hasPosition {
		logger.Info("[LivePipeline] Active position or order, skipping LLM.", "side", side)
		return nil
	}

	// --- 3.5) Cooldown check (หลัง upsert แล้ว ก่อน LLM) ---
	if isInCooldown {
		logger.Info("[LivePipeline] ⏸ in cooldown, skipping LLM + order",
			"bars_remaining", barsRemaining,
		)
		hooks.OnOrderExecuted(symbol, "HOLD", wsClose, "cooldown", "", "")
		return nil
	}

	_, roi, err := trade.CalculateDailyROI(binanceClient)
	if err != nil {
		logger.Error(fmt.Sprintf("[OrderExecution] Failed to calculate daily ROI: %v", err))
	} else {
		logger.Info(fmt.Sprintf("[OrderExecution] Current Daily ROI: %.2f%%", roi*100))
	}

	if roi <= cfg.Agent.StopLossROI {
		logger.Info("[LivePipeline] Daily ROI below stop loss threshold, skipping order execution", "roi", roi)
		hooks.OnOrderExecuted(symbol, "HOLD", wsClose, "stop loss triggered", "", "")
		return nil
	}
	if roi >= cfg.Agent.StopROI {
		logger.Info("[LivePipeline] Daily ROI above stop profit threshold, skipping order execution", "roi", roi)
		hooks.OnOrderExecuted(symbol, "HOLD", wsClose, "stop profit triggered", "", "")
		return nil
	}

	// --- 3.9) Pre-filter gate — skip LLM on low-edge bars ---
	pfResult := prefilter.RunPrefilter(prefilter.Input{
		Candles:   wsRestCandle,
		Threshold: cfg.LLM.PrefilterThreshold,
	})
	if !pfResult.PassThreshold {
		logger.Info("[LivePipeline] prefilter skip — emitting local HOLD",
			"score", pfResult.Score,
			"reason", pfResult.SkipReason,
		)
		hooks.OnOrderExecuted(symbol, "HOLD", wsClose, "prefilter: "+pfResult.SkipReason, "", "")
		return nil
	}

	// --- 4) LLM ---
	llmOutput, err := NewLLMPatternAgent(
		ctx, binanceClient, *logger, cfg, cfg.Database, cfg.OpenRouter,
		symbol, interval, wsRestCandle, feature.Embedding, cfg.LLM.TopN,
	)
	if err != nil {
		hooks.OnPipelineError("llm", err)
		return fmt.Errorf("[LivePipeline] llm: %w", err)
	}
	logger.Info(fmt.Sprint("Result from Agent: ", llmOutput))

	signalLog := postgresql.TradeSignalLog{
		Time:            feature.Time,
		Symbol:          symbol,
		Interval:        interval,
		Signal:          llmOutput.Signal,
		Confidence:      llmOutput.Confidence,
		RegimeRead:      llmOutput.RegimeRead,
		PatternRead:     llmOutput.PatternRead,
		PriceActionRead: llmOutput.PriceActionRead,
		Synthesis:       llmOutput.Synthesis,
		RiskNote:        llmOutput.RiskNote,
		Invalidation:    llmOutput.Invalidation,
		WsClose:         wsClose,
	}

	// fire-and-forget log insert — ไม่ block order path
	go func() {
		// ใช้ context ใหม่ เผื่อ parent ctx ถูก cancel หลัง return
		logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := dbIngest.InsertTradeSignal(logCtx, signalLog); err != nil {
			logger.Error("[LivePipeline] insert trade signal log", "err", err)
			return
		}
		logger.Info("[LivePipeline] Inserted trading log")
	}()

	// --- ต่อไปคือ order path ที่ไม่มีอะไรบล็อก ---
	if llmOutput.Confidence < cfg.LLM.ConfidenceThreshold {
		logger.Info("[LivePipeline] Low confidence, skipping order execution", "confidence", llmOutput.Confidence)
		hooks.OnOrderExecuted(symbol, "HOLD", wsClose, "low confidence", "", "")
		return nil
	}

	if err := NewOrderExecutionPipeline(ctx, *logger, binanceClient, symbol, llmOutput.Signal, wsClose); err != nil {
		hooks.OnPipelineError("order", err)
		return fmt.Errorf("[LivePipeline] order execution: %w", err)
	}

	hooks.OnOrderExecuted(symbol, llmOutput.Signal, wsClose, llmOutput.Synthesis, llmOutput.PatternRead, llmOutput.PriceActionRead)

	return nil
}

// parseBinanceInterval converts Binance interval strings (e.g. "1d", "1w") that
// time.ParseDuration cannot handle into valid Go duration strings.
func parseBinanceInterval(s string) (time.Duration, error) {
	r := strings.NewReplacer("1d", "24h", "2d", "48h", "3d", "72h", "1w", "168h")
	return time.ParseDuration(r.Replace(s))
}
