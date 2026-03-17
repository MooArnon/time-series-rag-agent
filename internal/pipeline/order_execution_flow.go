package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"

	"github.com/adshao/go-binance/v2/futures"
)

func NewOrderExecutionPipeline(logger slog.Logger, futureClient *futures.Client, symbol string, signal string, priceToOpen float64) error {
	conf := config.LoadConfig()
	executor := exchange.NewExecutor(
		futureClient,
		symbol,
		conf.Agent.AviableTradeRatio,
		conf.Agent.Leverage,
		conf.Agent.SLPercentage,
		conf.Agent.TPPercentage,
		logger,
	)
	if signal == "SHORT" || signal == "LONG" {

		tradeCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := executor.PlaceTrade(tradeCtx, signal, priceToOpen); err != nil {
			logger.Error(fmt.Sprintf("[OrderExecution] PlaceTrade failed: %v", err))
			return err
		}
		err := executor.PlaceTrade(tradeCtx, signal, priceToOpen)
		if err != nil {
			logger.Info(fmt.Sprintln(err))
		}
	} else {
		logger.Info("[OrderExecution] HOLD - checking for stale open orders...")
		tradeCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

		// ← defer ให้ run หลัง function จบ
		defer cancel()

		err := executor.CancelTrade(tradeCtx)
		if err != nil {
			logger.Error(fmt.Sprintf("CancelTrade failed: %v", err))
		} else {
			logger.Info("[OrderExecution] Stale order cancelled successfully")
		}
	}
	return nil
}
