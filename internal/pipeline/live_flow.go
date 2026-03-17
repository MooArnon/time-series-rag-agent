package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/llm"
	"time-series-rag-agent/internal/storage/postgresql"

	"github.com/adshao/go-binance/v2/futures"
	"golang.org/x/sync/errgroup"
)

func NewLivePipeline(ctx context.Context, logger *slog.Logger, wsCandle []exchange.WsCandle, symbol string, interval string, vectorSize int, wsClose float64) error {
	logger.Info("[LivePipeline] Starting Embedding Pipeline")
	cfg := config.LoadConfig()
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	adapter := exchange.NewBinanceAdapter(binanceClient)

	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser, cfg.Database.DBPassword,
		cfg.Database.DBHost, cfg.Database.DBPort, cfg.Database.DBName,
	)

	// --- 1) REST fetch + DB connect in parallel (fail-fast) ---
	var (
		restCandle []exchange.RestCandle
		dbIngest   *postgresql.PatternStore
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

	if err := g1.Wait(); err != nil {
		if dbIngest != nil {
			dbIngest.Close()
		}
		return fmt.Errorf("[LivePipeline] init: %w", err)
	}
	defer dbIngest.Close()

	// --- 2) Embedding (sequential, depends on restCandle + dbIngest) ---
	feature, label, wsRestCandle := NewEmbeddingPipeline(*logger, wsCandle, restCandle, vectorSize, symbol, interval)
	if feature == nil {
		return fmt.Errorf("[LivePipeline] feature is nil")
	}

	// --- 3) DB upserts + LLM agent in parallel (fail-fast) ---
	var llmOutput llm.TradeSignal

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

	g2.Go(func() error {
		var err error
		llmOutput, err = NewLLMPatternAgent(
			ctx2, binanceClient, *logger, cfg, cfg.Database, cfg.OpenRouter,
			symbol, interval, wsRestCandle, feature.Embedding, cfg.LLM.TopN,
		)
		return err
	})

	if err := g2.Wait(); err != nil {
		return fmt.Errorf("[LivePipeline] phase 2: %w", err)
	}
	logger.Info(fmt.Sprint("Result from Agent: ", llmOutput))

	// --- 4) Order execution (sequential, depends on LLM signal) ---
	if err := NewOrderExecutionPipeline(ctx, *logger, binanceClient, symbol, llmOutput.Signal, wsClose); err != nil {
		return fmt.Errorf("[LivePipeline] order execution: %w", err)
	}

	return nil
}
