package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"

	"time-series-rag-agent/internal/ai"
)

// --- Configuration ---
const (
	Symbol       = "ADAUSDT"
	Interval     = "1m" // Binance string
	IntervalSecs = 60   // Used for math checks (1m = 60s)
	VectorWindow = 60   // N candles for the pattern
)

func main() {
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
		
		// 1. Parse Live Candle
		// Binance times are in Milliseconds -> Convert to Seconds
		openTimeSec := event.Kline.StartTime / 1000
		closePrice, _ := strconv.ParseFloat(event.Kline.Close, 64)

		liveCandle := ai.InputData{
			Time:  openTimeSec, 
			Close: closePrice,
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

		if feature != nil {
			// Success!
			fmt.Printf("✅ Feature Ready in %v | Embedding Size: %d\n", time.Since(start), len(feature.Embedding))
			
			// Optional: Print first 3 embedding values to verify it's working
			if len(feature.Embedding) >= 3 {
				fmt.Printf("   Vec: [%.4f, %.4f, %.4f ...]\n", feature.Embedding[0], feature.Embedding[1], feature.Embedding[2])
			}
		}

		// --- Calculate Labels (For Database) ---
		// "What 'Truths' did we just learn about the past?"
		labels := agent.CalculateLabels(cleanWindow)
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

// fetchRealHistory calls the actual Binance Futures API
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
		// Parse Open Time (ms -> s)
		// Note: Binance returns OpenTime. Technical Analysis usually aligns on OpenTime.
		openTime := k.OpenTime / 1000
		
		// Parse Close Price (string -> float)
		closePrice, err := strconv.ParseFloat(k.Close, 64)
		if err != nil {
			return nil, fmt.Errorf("bad float price: %v", err)
		}

		data[i] = ai.InputData{
			Time:  openTime,
			Close: closePrice,
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