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
	CANDLE_FILE_NAME       = "candle.png"
	LATEST_CANDLE_PLOT     = 45
	TRADING_LOOK_BACK_DAYS = 2
	TopN1H                 = 10
)

func NewLLMPatternAgent(ctx context.Context, futureClient *futures.Client, logger slog.Logger, appConfig *config.AppConfig, dbConfig config.DatabaseConfig, openRouterConfig config.OpenRouterConfig, symbol string, interval string, candel []exchange.WsRestCandle, feature []float64, topN int) (llm.TradeSignal, error) {
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		dbConfig.DBUser,
		dbConfig.DBPassword,
		dbConfig.DBHost,
		dbConfig.DBPort,
		dbConfig.DBName,
	)
	db, err := postgresql.NewPostgresDB(ctx, connString, logger)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Cannot establish connection for candle ingestion.")
		return llm.TradeSignal{}, err
	}
	defer db.Close()

	patterns, err := db.QueryTopN(ctx, symbol, interval, feature, topN)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error from query Top n")
		return llm.TradeSignal{}, err
	}

	patterns1h, err := db.QueryTopN(ctx, symbol, "1h", feature, TopN1H)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error from query Top n")
		return llm.TradeSignal{}, err
	}

	plot.GenerateCandleChart(candel, CANDLE_FILE_NAME, LATEST_CANDLE_PLOT)
	logger.Info("[LLMPatternPipeline] Finished plot")

	llmService := llm.NewLLMService(openRouterConfig.ApiKey, appConfig.LLM.MaxDailyTokens)
	regime, err := exchange.FetchLatestRegimes(logger, futureClient, appConfig, symbol, []string{"4h", "1d"})
	if err != nil {
		logger.Error("[LLMPatternPipeline] Regime fetching")
		return llm.TradeSignal{}, err
	}

	currentTimestamp := time.Now().UTC().Format("2006-01-02 15:04:05")

	dailyPnL, roi, err := trade.CalculateDailyROI(futureClient)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error at PnL calculation")
		return llm.TradeSignal{}, err
	}

	tradeHistory, err := trade.GetPositionHistory(futureClient, symbol, TRADING_LOOK_BACK_DAYS)
	if err != nil {
		logger.Error("[LLMPatternPipeline] Error at position history")
		return llm.TradeSignal{}, err
	}
	promptPositions := tradeHistory
	if len(promptPositions) > appConfig.LLM.LimitTradeHistory {
		promptPositions = promptPositions[:appConfig.LLM.LimitTradeHistory]
	}

	logger.Info(fmt.Sprintf("Current ROI=%f, PnL=%f", roi, dailyPnL))

	systemMessage, userContent, b64Candle, err := llmService.GenerateTradingPrompt(currentTimestamp, patterns, patterns1h, CANDLE_FILE_NAME, promptPositions, regime, dailyPnL)
	if err != nil {
		logger.Error(fmt.Sprintf("Prompt Error: %v", err))
		return llm.TradeSignal{}, err
	}
	logger.Info("[LLMPatternPipeline] systemMessage", "msg", systemMessage)
	logger.Info("[LLMPatternPipeline] userContent", "msg", userContent)

	signal, err := llmService.GenerateSignal(ctx, systemMessage, userContent, b64Candle)
	if err != nil {
		logger.Error(fmt.Sprintf("LLM Error: %v", err))
		return llm.TradeSignal{}, err
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
