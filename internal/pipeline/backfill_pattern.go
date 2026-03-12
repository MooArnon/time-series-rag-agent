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

func NewBackfillPipeline(logger slog.Logger, symbol string, interval string, limit int, vectorWindow int, dayLookback int) error {
	logger.Info("[LivePipeline] Starting Embedding Pipeline")
	cfg := config.LoadConfig()
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -dayLookback)
	restCandle, err := exchange.FetchHistoryByTime(binanceClient, symbol, interval, startTime, endTime)
	if err != nil {
		logger.Error(fmt.Sprintln("[LivePipeline] Error at rest candle fetched: ", err))
		return err
	}

	feature, label := NewBackfillEmbeddingPipeline(logger, restCandle, symbol, interval, vectorWindow)

	// -- Save to DB -- //
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		cfg.Database.DBPort,
		cfg.Database.DBName,
	)
	db, err := postgresql.NewPostgresDB(connString, logger)
	if err != nil {
		logger.Error(fmt.Sprintln("[LivePipeline] Error at PostgreSQL connection: ", err))
		return err
	}
	dbCtx := context.TODO()
	db.BulkUpsertFeature(dbCtx, feature)
	logger.Info("[LivePipeline] Ingested feature")
	db.UpsertLabels(dbCtx, symbol, interval, label)
	logger.Info("[LivePipeline] Ingested label")

	return nil
}
