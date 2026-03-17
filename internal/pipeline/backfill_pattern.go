package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/storage/postgresql"

	"github.com/adshao/go-binance/v2/futures"
)

func NewBackfillPipeline(ctx context.Context, logger *slog.Logger, symbol string, interval string, limit int, vectorWindow int, dayLookback int) error {
	logger.Info("[BackfillPipeline] Starting Embedding Pipeline")
	cfg := config.LoadConfig()
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -dayLookback)
	restCandle, err := exchange.FetchHistoryByTime(binanceClient, symbol, interval, startTime, endTime)
	if err != nil {
		logger.Error(fmt.Sprintf("[BackfillPipeline] REST candle fetch: %v", err))
		return err
	}

	feature, label := NewBackfillEmbeddingPipeline(*logger, restCandle, symbol, interval, vectorWindow)

	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		cfg.Database.DBPort,
		cfg.Database.DBName,
	)
	db, err := postgresql.NewPostgresDB(ctx, connString, *logger)
	if err != nil {
		logger.Error(fmt.Sprintf("[BackfillPipeline] DB connection: %v", err))
		return err
	}
	defer db.Close()

	if err := db.BulkUpsertFeature(ctx, feature); err != nil {
		logger.Error(fmt.Sprintf("[BackfillPipeline] BulkUpsertFeature: %v", err))
		return err
	}
	logger.Info("[BackfillPipeline] Ingested feature")

	if err := db.UpsertLabels(ctx, symbol, interval, label); err != nil {
		logger.Error(fmt.Sprintf("[BackfillPipeline] UpsertLabels: %v", err))
		return err
	}
	logger.Info("[BackfillPipeline] Ingested label")

	return nil
}
