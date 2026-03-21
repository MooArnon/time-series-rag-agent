package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
	pkg "time-series-rag-agent/pkg/notifier"
)

const (
	SYMBOL             = "BTCUSDT"
	INTERVAL           = "15m"
	VECTOR_SIZE        = 30
	CONSECUTIVE_LOSSES = 2
)

// cmd/live/main.go
func main() {
	logger := logger.SetupLogger()
	logger.Info("[Entrypoint] Start live data streaming")
	cfg := config.LoadConfig()

	logger.Info(fmt.Sprintf("[Entrypoint] leverage: %d", cfg.Agent.Leverage))

	discord := pkg.NewDiscordClient(cfg.Discord.DISCORD_NOTIFY_WEBHOOK_URL, cfg.Discord.DISCORD_NOTIFY_WEBHOOK_URL)
	hooks := discord.NewPipelineHooks(SYMBOL, INTERVAL)

	binanceClient, err := exchange.NewBinanceClient(context.Background(), cfg)
	if err != nil {
		logger.Info("[Entrypoint] Error at Binance client initiate")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	exchange.StartKlineWebsocket(ctx, SYMBOL, INTERVAL, logger, func(candle exchange.WsCandle) {
		logger.Info("[Entrypoint] received candle", "time", candle.Time, "close", candle.Close)

		candleArray := []exchange.WsCandle{candle}
		if err := pipeline.NewLivePipeline(ctx, logger, binanceClient, hooks, candleArray, SYMBOL, INTERVAL, VECTOR_SIZE, candle.Close); err != nil {
			logger.Error(fmt.Sprintf("[Entrypoint] Live pipeline error: %v", err))
			return
		}

		logger.Info("[Entrypoint] Finished live pipeline")
	})

	logger.Info("shutdown complete")
}
