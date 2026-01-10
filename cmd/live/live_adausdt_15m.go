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
	"time-series-rag-agent/internal/llm"
	"time-series-rag-agent/internal/plot"
)

// --- Configuration ---
const (
	Symbol       = "ADAUSDT"
	Interval     = "1m" // Binance string
	IntervalSecs = 60   // * 15 // Used for math checks (1m = 60s)
	VectorWindow = 60   // N candles for the pattern
	top_k        = 12
)

func main() {
	cfg := config.LoadConfig()
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		cfg.Database.DBPort,
		cfg.Database.DBName,
	)
	pg, err := database.NewPostgresDB(connString)
	if err != nil {
		log.Fatalf("❌ Database Connection Failed: %v", err)
	}
	defer pg.Close()

	fmt.Println("✅ Connected to Postgres & pgvector")

	// ========================================================================
	//  Websocket to gather data
	// ========================================================================
	agent := ai.NewPatternAI(Symbol, Interval, "v1", VectorWindow)
	client := futures.NewClient("", "")

	// Start WebSocket Listener
	fmt.Printf("--- Connecting to Binance Futures [%s @ %s] ---\n", Symbol, Interval)

	// Create a channel to keep main alive (or use a signal handler)
	doneC := make(chan struct{})

	errHandler := func(err error) {
		log.Printf("WebSocket Error: %v", err)
	}

	wsHandler := func(event *futures.WsKlineEvent) {

		// We ONLY care when the candle is closed (IsFinal = true)
		if !event.Kline.IsFinal {
			return
		}

		// ---  HOT PATH: Candle Just Closed ---
		// event.Kline.IsFinal is True
		start := time.Now()

		liveCandle := ai.InputData{
			Time:  event.Kline.StartTime / 1000,
			Open:  parse(event.Kline.Open),
			High:  parse(event.Kline.High),
			Low:   parse(event.Kline.Low),
			Close: parse(event.Kline.Close),
		}

		fmt.Printf("\n[Event] Candle Closed: %s | Price: %.4f\n",
			time.Unix(liveCandle.Time, 0).Format("15:04:05"),
			liveCandle.Close,
		)

		// 2. Fetch History via REST
		// We request Window + 5 to handle overlaps safely
		history, err := fetchRealHistory(client, Symbol, Interval, VectorWindow+5)
		if err != nil {
			log.Printf("❌ API Error: %v", err)
			return
		}

		// 3. Safe Merge (Deduplication & Gap Check)
		cleanWindow, err := SafeMerge(history, liveCandle, VectorWindow, IntervalSecs)
		if err != nil {
			log.Printf("⚠️ Data Integrity Skip: %v", err)
			return
		}

		// 4. Calculate Features
		feature := agent.CalculateFeatures(cleanWindow)
		if feature == nil {
			return
		}
		fmt.Printf("✅ Feature Ready in %v | Embedding Size: %d\n", time.Since(start), len(feature.Embedding))

		matches, err := pg.SearchPatterns(context.Background(), feature.Embedding, top_k, Symbol)
		if len(matches) > 0 {
			log.Printf("🔎 Found %d matches. Visualizing alignment...", len(matches))

			// FIX: Pass feature.Embedding (Current) and matches (Historical)
			//filename := fmt.Sprintf("chart_%s.png", time.Now().Format("150405"))
			const fileProj string = "chart.png"

			err := plot.GeneratePredictionChart(feature.Embedding, matches, fileProj)

			if err != nil {
				log.Printf("❌ Plot Error: %v", err)
			} else {
				log.Printf("📊 Chart saved: %s", fileProj)
			}

			// filename_cancdle_chart := fmt.Sprintf("candle_chart_%s.png", time.Now().Format("150405"))
			const fileCandle string = "candle.png"
			err_candle_chart := plot.GenerateCandleChart(cleanWindow, fileCandle)
			if err != nil {
				log.Printf("❌ Plot Error: %v", err_candle_chart)
			} else {
				log.Printf("📊 Chart saved: %s", fileCandle)
			}

			llmClient := llm.NewLLMService(cfg.OpenRouter.ApiKey)
			sysMsg, usrMsg, b64A, b64B, err := llmClient.GenerateTradingPrompt(
				time.Now().Format("15:04:05"),
				matches,
				fileProj,   // Chart A (Macro)
				fileCandle, // Chart B (Micro)
			)
			if err != nil {
				log.Printf("❌ Prompt Error: %v", err)
				return
			}
			log.Println("b64A", b64A)
			log.Println("b64B", b64B)
			log.Println("sysMsg", sysMsg)
			log.Println("usrMsg", usrMsg)
		}
		go func(feat *ai.PatternFeature, window []ai.InputData) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// A. Calculate Labels (The "Truth" about the past)
			labels := agent.CalculateLabels(window)

			// B. Ingest (Insert T, Update T-n)
			err := pg.IngestPattern(ctx, feat, labels)
			if err != nil {
				log.Printf("⚠️ Ingestion Failed: %v", err)
			} else {
				log.Printf("💾 [Ingest] Saved T (%s) & Updated %d Past Labels",
					feat.Time.Format("15:04"), len(labels))
			}
		}(feature, cleanWindow) // Pass copies/pointers safely
	}

	// Start the stream
	done, _, err := futures.WsKlineServe(Symbol, Interval, wsHandler, errHandler)
	if err != nil {
		log.Fatalf("Failed to connect WS: %v", err)
	}

	<-done
	<-doneC
}

// --- Real Infrastructure Helpers ---

func fetchRealHistory(client *futures.Client, symbol string, interval string, limit int) ([]ai.InputData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Call /fapi/v1/klines
	klines, err := client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	// Convert Binance Response -> []ai.InputData
	data := make([]ai.InputData, len(klines))
	for i, k := range klines {
		// 1. Parse TIME
		openTime := k.OpenTime / 1000

		// 2. Parse ALL Prices (Open, High, Low, Close)
		// Crucial: You must parse these, or they default to 0.0
		op, _ := strconv.ParseFloat(k.Open, 64)
		hi, _ := strconv.ParseFloat(k.High, 64)
		lo, _ := strconv.ParseFloat(k.Low, 64)
		cl, _ := strconv.ParseFloat(k.Close, 64)

		data[i] = ai.InputData{
			Time:  openTime,
			Open:  op, // <--- This was missing
			High:  hi, // <--- This was missing
			Low:   lo, // <--- This was missing
			Close: cl,
		}
	}

	return data, nil
}

// SafeMerge handles the deduplication and continuity check (Unchanged logic)
func SafeMerge(history []ai.InputData, live ai.InputData, reqWindow int, intervalSecs int64) ([]ai.InputData, error) {
	cleanHistory := []ai.InputData{}

	// Step A: Filter Overlaps (Keep strictly older candles)
	for _, h := range history {
		if h.Time < live.Time {
			cleanHistory = append(cleanHistory, h)
		}
	}

	// Step B: Check length
	if len(cleanHistory) < reqWindow {
		return nil, fmt.Errorf("not enough history after filtering. Need %d, got %d", reqWindow, len(cleanHistory))
	}

	// Step C: Slice exact window
	neededHistory := cleanHistory[len(cleanHistory)-reqWindow:]

	// Step D: Verify Continuity (Gap Check)
	lastHistTime := neededHistory[len(neededHistory)-1].Time
	diff := live.Time - lastHistTime

	if diff != intervalSecs {
		return nil, fmt.Errorf("GAP! HistEnd: %d, Live: %d, Diff: %d (Expected %d)",
			lastHistTime, live.Time, diff, intervalSecs)
	}

	// Step E: Combine
	return append(neededHistory, live), nil
}

func parse(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
