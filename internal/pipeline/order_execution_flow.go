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

func NewOrderExecutionPipeline(ctx context.Context, logger slog.Logger, futureClient *futures.Client, symbol string, signal string, priceToOpen float64) error {
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

	tradeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if signal == "SHORT" || signal == "LONG" {
		if err := executor.SetLeverage(tradeCtx, conf.Agent.Leverage); err != nil {
			logger.Error(fmt.Sprintf("[OrderExecution] SetLeverage failed: %v", err))
			return err
		}
		if err := executor.PlaceTrade(tradeCtx, signal, priceToOpen); err != nil {
			logger.Error(fmt.Sprintf("[OrderExecution] PlaceTrade failed: %v", err))
			return err
		}
	} else {
		logger.Info("[OrderExecution] HOLD - checking for stale open orders...")
		if err := executor.CancelTrade(tradeCtx); err != nil {
			logger.Error(fmt.Sprintf("CancelTrade failed: %v", err))
			return err
		}
		logger.Info("[OrderExecution] Stale order cancelled successfully")
	}

	return nil
}
