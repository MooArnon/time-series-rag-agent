package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/trade"

	"github.com/adshao/go-binance/v2/futures"
)

func NewOrderExecutionPipeline(ctx context.Context, logger slog.Logger, futureClient *futures.Client, symbol string, signal string, priceToOpen float64) error {
	conf := config.LoadConfig()

	_, roi, err := trade.CalculateDailyROI(futureClient)
	if err != nil {
		logger.Error(fmt.Sprintf("[OrderExecution] Failed to calculate daily ROI: %v", err))
	} else {
		logger.Info(fmt.Sprintf("[OrderExecution] Current Daily ROI: %.2f%%", roi*100))
	}

	var AviableTradeRatio float64
	if roi >= conf.Agent.ReduceRoiTrigger {
		AviableTradeRatio = conf.Agent.ReductionAviableTradeRatio
	} else {
		AviableTradeRatio = conf.Agent.AviableTradeRatio
	}

	executor := exchange.NewExecutor(
		futureClient,
		symbol,
		AviableTradeRatio,
		conf.Agent.Leverage,
		conf.Agent.SLPercentage,
		conf.Agent.TPPercentage,
		logger,
	)

	tradeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	switch signal {
	case "SHORT", "LONG":
		if err := executor.SetLeverage(tradeCtx, conf.Agent.Leverage); err != nil {
			logger.Error(fmt.Sprintf("[OrderExecution] SetLeverage failed: %v", err))
			return err
		}
		if err := executor.PlaceTrade(tradeCtx, signal, priceToOpen); err != nil {
			logger.Error(fmt.Sprintf("[OrderExecution] PlaceTrade failed: %v", err))
			return err
		}
	case "HOLD":
		logger.Info("[OrderExecution] HOLD - checking for stale open orders...")
		if err := executor.CancelTrade(tradeCtx); err != nil {
			logger.Error(fmt.Sprintf("CancelTrade failed: %v", err))
			return err
		}
		logger.Info("[OrderExecution] Stale order cancelled successfully")
	default:
		return fmt.Errorf("unknown signal %q: refusing to modify orders", signal)
	}

	return nil
}
