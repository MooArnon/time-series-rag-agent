package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/storage/postgresql"

	"github.com/adshao/go-binance/v2/futures"
)

func NewLivePipeline(logger slog.Logger, wsCandle []exchange.WsCandle, symbol string, interval string, vectorSize int) error {
	logger.Info("[LivePipeline] Starting Embedding Pipeline")
	cfg := config.LoadConfig()
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	adapter := exchange.NewBinanceAdapter(binanceClient)

	// +99 to buffer at plot
	restCandle, err := exchange.FetchLatestCandles(adapter, symbol, interval, vectorSize+1+99)
	if err != nil {
		logger.Error(fmt.Sprintln("[LivePipeline] Error at rest candle fetched: ", err))
		return err
	}
	logger.Info(fmt.Sprintln("[LivePipeline] candle: ", restCandle))

	// -- Embedding -- //
	feature, label, wsRestCandle := NewEmbeddingPipeline(logger, wsCandle, restCandle, vectorSize, symbol, interval)
	if feature == nil {
		logger.Error("[LivePipeline] feature is nil, skipping upsert")
		return fmt.Errorf("feature is nil")
	}
	logger.Info(fmt.Sprint("[LivePipeline] Feature: ", feature))
	logger.Info(fmt.Sprint("[LivePipeline] Label: ", label))

	// -- Save to DB -- //
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		cfg.Database.DBPort,
		cfg.Database.DBName,
	)
	dbIngest, err := postgresql.NewPostgresDB(connString, logger)
	if err != nil {
		logger.Error(
			fmt.Sprintln(
				"[LivePipeline] Cannot establish connection for candle ingestion: ",
				err,
			),
		)
	}
	defer dbIngest.Close()
	if err != nil {
		logger.Error(fmt.Sprintln("[LivePipeline] Error at PostgreSQL connection: ", err))
		return err
	}
	dbCtx := context.TODO()
	dbIngest.UpsertFeature(dbCtx, *feature)
	logger.Info("[LivePipeline] Ingested feature")
	dbIngest.UpsertLabels(dbCtx, symbol, interval, label)
	logger.Info("[LivePipeline] Ingested label")

	output, err := NewLLMPatternAgent(binanceClient, logger, cfg, cfg.Database, cfg.OpenRouter, symbol, interval, wsRestCandle, feature.Embedding, cfg.LLM.TopN)
	if err != nil {
		logger.Error(
			fmt.Sprintln("[LivePipeline] Error with ", err),
		)
		return err
	}

	fmt.Print(output)

	return nil
}
