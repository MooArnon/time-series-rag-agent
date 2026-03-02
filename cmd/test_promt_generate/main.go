package main

import (
	"fmt"
	"time"
	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/database"
	"time-series-rag-agent/internal/llm"
	market_trend "time-series-rag-agent/internal/market_trend"

	"context"

	"github.com/adshao/go-binance/v2/futures"
)

func main() {
	const fileProj string = "chart.png"
	const fileCandle string = "candle.png"

	cfg := config.LoadConfig()
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		cfg.Database.DBPort,
		cfg.Database.DBName,
	)
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	llmClient := llm.NewLLMService(cfg.OpenRouter.ApiKey)

	db, err := database.NewPostgresDB(connString)
	if err != nil {
		fmt.Printf("Error creating PostgresDB: %v\n", err)
		return
	}

	cnt := context.TODO()

	// Promt generate flow:

	// 1. Query recent PnL data (past n days)
	pnlData, err := db.QueryPnLData(cnt, cfg.LLM.NumPnLLookback)
	if err != nil {
		fmt.Printf("Error querying PnL data: %v\n", err)
		return
	}

	Embedding := []float64{-0.2508807, -0.47536245, 1.5144132, 1.0752412, -0.8346041, -0.351039, 0.20079435, -0.78024566, 1.6609708, -1.2521714, -0.5744761, 2.3229225, -0.32652187, -0.6946624, -1.250709, -1.2465832, -0.6693986, 0.54001486, -0.2146003, 0.8676081, 0.94825107, 0.9481427, 1.2251079, 0.24620001, 0.42551753, -0.09951484, 0.5001839, -0.249815, -1.1046994, -2.1000843} // Placeholder embedding vector
	matches, err := db.SearchPatterns(context.Background(), Embedding, 12, "ETHUSDT")
	regimes, _ := market_trend.FetchLatestRegimes(binanceClient, cfg, "ETHUSDT", []string{"4h", "1d"})

	sysMsg, usrMsg, _, _, err := llmClient.GenerateTradingPrompt(
		time.Now().Format("15:04:05"),
		matches,
		fileProj,   // Chart A (Macro)
		fileCandle, // Chart B (Micro)
		pnlData,
		regimes,
	)
	if err != nil {
		fmt.Printf("Error generating trading prompt: %v\n", err)
		return
	}

	fmt.Println("sysMsg: ", sysMsg)
	fmt.Println("usrMsg: ", usrMsg)

	// Request PnL data for the past n days

	// Request HOLD position for past n days

	// Generate promt
}
