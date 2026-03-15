package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/llm"
	"time-series-rag-agent/internal/plot"
	"time-series-rag-agent/internal/storage/postgresql"
	"time-series-rag-agent/internal/trade"

	"github.com/adshao/go-binance/v2/futures"
)

const (
	CANDLE_FILE_NAME   = "candle.png"
	CHART_FILE_NAME    = "chart.png"
	LATEST_CANDLE_PLOT = 45
)

func NewLLMPatternAgent(futureClient *futures.Client, logger slog.Logger, appConfig *config.AppConfig, dbConfig config.DatabaseConfig, llmConfig config.OpenRouterConfig, symbol string, interval string, candel []exchange.WsRestCandle, feature []float64, topN int) (llm.TradeSignal, error) {
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
		return llm.TradeSignal{}, err
	}
	defer db.Close()

	patterns, err := db.QueryTopN(context.TODO(), symbol, interval, feature, topN)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error from query Top n")
		return llm.TradeSignal{}, err
	}

	plot.GenerateCandleChart(candel, CANDLE_FILE_NAME, LATEST_CANDLE_PLOT)
	plot.GeneratePredictionChart(feature, patterns, CHART_FILE_NAME)
	logger.Info("[LLMPatternPipeline] Finshed plot")

	llmService := llm.NewLLMService(llmConfig.ApiKey)
	regime, err := exchange.FetchLatestRegimes(logger, futureClient, appConfig, symbol, []string{"4h", "1d"}, candel)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Regime fetching")
		return llm.TradeSignal{}, err
	}

	currentTimestamp := time.Now().UTC().Format("2025-01-01 15:00:00")
	dailyPnL, roi, err := trade.CalculateDailyROI(futureClient)
	tradeHistory, err := trade.GetPositionHistory(futureClient, symbol, 30)
	logger.Info(fmt.Sprintf("Current ROI=%f, PnL=%f", roi, dailyPnL))
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error at PnL generates")
		return llm.TradeSignal{}, err
	}
	// systemMessage, userContent, b64Pattern, b64Canle, err := llmService.GenerateTradingPrompt(currentTimestamp, patterns, CHART_FILE_NAME, CANDLE_FILE_NAME, tradeHistory, regime, dailyPnL)
	systemMessage, userContent, b64Pattern, b64Canle, err := llmService.GenerateTradingPrompt(currentTimestamp, patterns, CHART_FILE_NAME, CANDLE_FILE_NAME, tradeHistory, regime, dailyPnL)

	if err != nil {
		logger.Info(fmt.Sprintf("Prompt Error: %v", err))
		return llm.TradeSignal{}, nil
	}

	signal, err := llmService.GenerateSignal(context.Background(), systemMessage, userContent, b64Pattern, b64Canle)
	if err != nil {
		logger.Info(fmt.Sprintf("LLM Error: %v", err))
		return llm.TradeSignal{}, nil
	}
	logger.Info("Signal result",
		"signal", signal.Signal,
		"confidence", signal.Confidence,
		"regime_read", signal.RegimeRead,
		"pattern_read", signal.PatternRead,
		"price_action_read", signal.PriceActionRead,
		"synthesis", signal.Synthesis,
		"risk_note", signal.RiskNote,
		"invalidation", signal.Invalidation,
	)

	return *signal, nil
}
