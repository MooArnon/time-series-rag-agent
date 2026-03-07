package pipeline

import (
	"fmt"
	"log/slog"
	"os"
	"time-series-rag-agent/internal/embedding"
	"time-series-rag-agent/internal/exchange"

	"github.com/adshao/go-binance/v2/futures"
)

func NewLivePipeline(logger slog.Logger, wsCandle []exchange.WsCandle, symbol string, interval string) error {
	logger.Info("[LivePipeline] Starting Embedding Pipeline")
	binanceClient := futures.NewClient(os.Getenv("BINANCE_API_KEY"), os.Getenv("BINANCE_SECRET"))
	adapter := exchange.NewBinanceAdapter(binanceClient)

	restCandle, err := exchange.FetchLatestCandles(adapter, "ETHUSDT", "15m", 30)
	if err != nil {
		logger.Error(fmt.Sprintln("[LivePipeline] Error at rest candle fetched: ", err))
		return err

	}
	logger.Info(fmt.Sprintln("[LivePipeline] candle: ", restCandle))

	// -- Features -- //
	fc := embedding.NewFeatureCalculator(symbol, interval, len(restCandle))
	wsRestCandle := embedding.MergeCandles(wsCandle, restCandle)
	feature := fc.Calculate(wsRestCandle)
	logger.Info(fmt.Sprint("[LivePipeline] Feature: ", feature))

	// -- Labels -- //
	lc := embedding.NewLabelCalculator()
	label := lc.CalculateFromHistory(wsRestCandle)
	logger.Info(fmt.Sprint("[LivePipeline] Label: ", label))

	return nil
}
