package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/plot"
	"time-series-rag-agent/internal/storage/postgresql"
)

const (
	CANDLE_FILE_NAME   = "candle.png"
	CHART_FILE_NAME    = "chart.png"
	LATEST_CANDLE_PLOT = 45
)

func NewLLMPatternAgent(logger slog.Logger, dbConfig config.DatabaseConfig, symbol string, interval string, candel []exchange.WsRestCandle, feature []float64, topN int) (LLMPatternPipelineOutPut, error) {
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		dbConfig.DBUser,
		dbConfig.DBPassword,
		dbConfig.DBHost,
		dbConfig.DBPort,
		dbConfig.DBName,
	)
	db, err := postgresql.NewPostgresDB(connString, logger)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Cannot establish connection for candle ingestion.")
		return LLMPatternPipelineOutPut{}, err
	}
	defer db.Close()

	patterns, err := db.QueryTopN(context.TODO(), symbol, interval, feature, topN)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error from query Top n")
		return LLMPatternPipelineOutPut{}, err
	}

	plot.GenerateCandleChart(candel, CANDLE_FILE_NAME, LATEST_CANDLE_PLOT)
	plot.GeneratePredictionChart(feature, patterns, CHART_FILE_NAME)
	logger.Info("[LLMPatternPipeline] Finshed plot")

	return LLMPatternPipelineOutPut{}, nil
}
