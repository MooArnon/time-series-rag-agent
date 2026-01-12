package main

import (
	"context"
	"fmt"
	"time"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/trade"

	"time-series-rag-agent/pkg"

	"github.com/adshao/go-binance/v2/futures"
)

const (
	Symbol            = "ETHUSDT"
	AviableTradeRatio = 0.85
	Leverage          = 10
	SLPercentage      = 0.03
	TPPercentage      = 0.1
	Signal            = "LONG"

	//! TODO HARDCODE
	priceToPlace = 3100.48
)

func main() {
	basicContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg := config.LoadConfig()
	logger := pkg.SetupLogger()

	// Initiate executor struct
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	executor := trade.NewExecutor(
		binanceClient,
		Symbol,
		AviableTradeRatio,
		Leverage,
		SLPercentage,
		TPPercentage,
		*logger,
	)

	if err := executor.SetLeverage(basicContext, executor.Leverage); err != nil {
		fmt.Println("Error syncing leverage:", err)
		return
	}

	// Check if have open order or not
	hasOpen, _, _, err := executor.HasOpenPosition(basicContext)
	if err != nil {
		fmt.Println(err)
	}

	if hasOpen != true {

		err = executor.PlaceTrade(basicContext, Signal, priceToPlace)
		if err != nil {
			fmt.Println(err)
		}
		return
	}
	fmt.Printf("Order is already opened")

}
