package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
	pkg "time-series-rag-agent/pkg/notifier"
)

const (
	INTERVAL    = "15m"
	VECTOR_SIZE = 30
)

var SYMBOLS = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "BNBUSDT"}

// cmd/live/main.go
func main() {
	logger := logger.SetupLogger()
	logger.Info("[Entrypoint] Start live data streaming")
	cfg := config.LoadConfig()

	logger.Info(fmt.Sprintf("[Entrypoint] leverage: %d", cfg.Agent.Leverage))

	discord := pkg.NewDiscordClient(cfg.Discord.DISCORD_NOTIFY_WEBHOOK_URL, cfg.Discord.DISCORD_NOTIFY_WEBHOOK_URL)

	binanceClient, err := exchange.NewBinanceClient(context.Background(), cfg)
	if err != nil {
		logger.Info("[Entrypoint] Error at Binance client initiate")
		return
	}
	logger.Info("[Entrypoint] Binance client ready — starting poll")

	adapter := exchange.NewBinanceAdapter(binanceClient)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var pipelineRunning atomic.Int32

	exchange.StartMultiSymbolKlineWebsocket(ctx, adapter, SYMBOLS, INTERVAL, logger, func(candles map[string]exchange.WsCandle) {
		if !pipelineRunning.CompareAndSwap(0, 1) {
			logger.Warn("[Entrypoint] previous pipeline still running, dropping bar")
			return
		}

		go func() {
			defer pipelineRunning.Store(0)

			winner, winnerCandle, ok := pipeline.SelectBestOpportunity(
				ctx, adapter, candles, SYMBOLS, INTERVAL, VECTOR_SIZE, cfg.LLM.PrefilterThreshold,
			)
			if !ok {
				logger.Info("[Entrypoint] no symbol passed prefilter — holding all")
				return
			}
			logger.Info("[Entrypoint] selected winner", "symbol", winner, "close", winnerCandle.Close)

			hooks := discord.NewPipelineHooks(winner, INTERVAL)
			if err := pipeline.NewLivePipeline(ctx, logger, binanceClient, hooks,
				[]exchange.WsCandle{winnerCandle}, winner, INTERVAL, VECTOR_SIZE, winnerCandle.Close,
			); err != nil {
				logger.Error(fmt.Sprintf("[Entrypoint] Live pipeline error: %v", err))
				return
			}
			logger.Info("[Entrypoint] Finished live pipeline", "symbol", winner)
		}()
	})

	logger.Info("shutdown complete")
}
