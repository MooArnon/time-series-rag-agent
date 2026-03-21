package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/adshao/go-binance/v2/futures"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/exchange"
)

// รัน: go run ./cmd/test/check_stoploss/
// ตั้ง env ก่อน:
//   export BINANCE_API_KEY=xxx
//   export BINANCE_API_SECRET=xxx
//   export BINANCE_TESTNET=true   (optional)

func main() {
	cfg := config.LoadConfig()
	apiKey := cfg.Market.ApiKey
	apiSecret := cfg.Market.ApiSecret
	if apiKey == "" || apiSecret == "" {
		fmt.Println("❌ BINANCE_API_KEY / BINANCE_API_SECRET not set")
		os.Exit(1)
	}

	if os.Getenv("BINANCE_TESTNET") == "true" {
		futures.UseTestnet = true
		fmt.Println("⚠️  using testnet")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	e := exchange.NewExecutor(
		futures.NewClient(apiKey, apiSecret),
		"BTCUSDT",
		0.95,
		5,
		0.05,
		0.05,
		*logger,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("🔍 checking last closed order...")

	wasSL, slTime, err := e.WasLastCloseStopLoss(ctx)
	if err != nil {
		fmt.Printf("❌ error: %v\n", err)
		os.Exit(1)
	}

	if wasSL {
		fmt.Println("🔴 last close was STOP LOSS")
		fmt.Println("slTime", slTime)
	} else {
		fmt.Println("🟢 last close was NOT stop loss (TP or unknown)")
	}
}
