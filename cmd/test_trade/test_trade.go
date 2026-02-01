package main

import (
	"fmt"
	"time-series-rag-agent/config"

	"time-series-rag-agent/internal/trade"

	"github.com/adshao/go-binance/v2/futures"
)

func main() {
	cfg := config.LoadConfig()

	// Initiate executor struct
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	pnl, roi, err := trade.CalculateDailyROI(binanceClient)
	if err != nil {
		fmt.Print(err)
	}
	fmt.Printf("ROI: %.3f | PnL: %f.3", roi, pnl)
}
