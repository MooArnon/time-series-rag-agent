package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
)

const (
	SYMBOL      = "BTCUSDT"
	INTERVAL    = "15m"
	VECTOR_SIZE = 30
)

// cmd/live/main.go
func main() {
	// 1. Logger
	logger := logger.SetupLogger()
	logger.Info("[Entrypoint] Start live data streaming")

	// 2. Graceful shutdown — รับ CTRL+C หรือ SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	exchange.StartKlineWebsocket(ctx, SYMBOL, INTERVAL, logger, func(candle exchange.WsCandle) {
		logger.Info("[Entrypoint] received candle", "time", candle.Time, "open", candle.Open, "high", candle.High, "low", candle.Low, "close", candle.Close, "volume", candle.Volume)

		candleArray := []exchange.WsCandle{candle}
		err := pipeline.NewLivePipeline(*logger, candleArray, SYMBOL, INTERVAL, VECTOR_SIZE, candle.Close)
		if err != nil {
			logger.Error(fmt.Sprintln("[Entrypoint] Error at live pipeline: ", err))
		}
		logger.Info("[Entrypoint] Sucessed live pipeline")
	})

	// ถึงตรงนี้ได้ = ctx ถูก cancel = shutdown gracefully
	logger.Info("shutdown complete")
}
