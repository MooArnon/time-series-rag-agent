package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/ai"
	"time-series-rag-agent/internal/database"
)

// Config for Backfill
const (
	Symbol       = "ADAUSDT"
	Interval     = "1m"
	VectorWindow = 60
	DaysToFetch  = 20 // How many days of history you want
)

func main() {
	// 1. Setup
	cfg := config.LoadConfig()
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser, cfg.Database.DBPassword, cfg.Database.DBHost, cfg.Database.DBPort, cfg.Database.DBName)

	db, err := database.NewPostgresDB(connString)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	client := futures.NewClient("", "") // Public client
	agent := ai.NewPatternAI(Symbol, Interval, "v1", VectorWindow)

	// 2. Calculate Time Range
	now := time.Now()
	startTime := now.AddDate(0, 0, -DaysToFetch).Unix() * 1000 // Binance needs ms
	endTime := now.Unix() * 1000

	fmt.Printf("--- 📥 Starting Backfill for %s [%s] ---\n", Symbol, Interval)
	fmt.Printf("Range: %s to %s\n", time.Unix(startTime/1000, 0), time.Unix(endTime/1000, 0))

	// 3. Fetch Loop (Pagination)
	var allCandles []ai.InputData
	currentStart := startTime

	for currentStart < endTime {
		// Fetch 1500 candles (Binance max)
		klines, err := client.NewKlinesService().
			Symbol(Symbol).
			Interval(Interval).
			StartTime(currentStart).
			Limit(1500).
			Do(context.Background())

		if err != nil {
			log.Fatalf("API Error: %v", err)
		}

		if len(klines) == 0 {
			break
		}

		// Parse
		for _, k := range klines {
			t := k.OpenTime / 1000 // ms -> s
			c, _ := strconv.ParseFloat(k.Close, 64)
			allCandles = append(allCandles, ai.InputData{Time: t, Close: c})
		}

		// Update cursor for next batch
		lastK := klines[len(klines)-1]
		currentStart = lastK.CloseTime + 1

		fmt.Printf("\rFetched %d candles... (Last: %s)", len(allCandles), time.Unix(lastK.OpenTime/1000, 0).Format("2006-01-02 15:04"))
		time.Sleep(100 * time.Millisecond) // Respect rate limits
	}
	fmt.Println("\n✅ Download Complete. Processing AI Models...")

	// 4. Generate Vectors & Labels
	// This runs your Go logic on the entire history
	bulkResults := agent.CalculateBulkData(allCandles)
	fmt.Printf("Generated %d patterns. Saving to DB...\n", len(bulkResults))

	// 5. Save to DB in Batches
	batchSize := 1000
	for i := 0; i < len(bulkResults); i += batchSize {
		end := i + batchSize
		if end > len(bulkResults) {
			end = len(bulkResults)
		}

		chunk := bulkResults[i:end]
		err := db.BulkSave(context.Background(), chunk)
		if err != nil {
			log.Printf("❌ Batch Error [%d-%d]: %v", i, end, err)
		} else {
			fmt.Printf("\rSaved %d/%d patterns...", end, len(bulkResults))
		}
	}

	fmt.Println("\n🎉 Backfill Done!")
}
