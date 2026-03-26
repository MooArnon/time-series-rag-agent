package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/storage/postgresql"
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

	duration, err := time.ParseDuration(interval)
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

	// --- 5) Order execution ---
	if err := NewOrderExecutionPipeline(ctx, *logger, binanceClient, symbol, llmOutput.Signal, wsClose); err != nil {
		hooks.OnPipelineError("order", err)
		return fmt.Errorf("[LivePipeline] order execution: %w", err)
	}

	hooks.OnOrderExecuted(symbol, llmOutput.Signal, wsClose, llmOutput.Synthesis, llmOutput.PatternRead, llmOutput.PriceActionRead)

	return nil
}
