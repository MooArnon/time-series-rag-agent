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
	"time-series-rag-agent/internal/plot"
	"time-series-rag-agent/pkg"
)

// --- Configuration ---
const (
	Symbol            = "ETHUSDT"
	Interval          = "1m" // Binance string
	IntervalSecs      = 60   // 15m = 60 * 15 // Used for math checks (1m = 60s)
	VectorWindow      = 30   // N candles for the pattern
	top_k             = 18
	AviableTradeRatio = 0.9
)

func main() {
	logger := pkg.SetupLogger()

	logger.Info(fmt.Sprintf("==== Proceed trading symbol: %s | interval: %s | TopK: %d ====", Symbol, Interval, top_k))
	cfg := config.LoadConfig()

	log.Println("[Initializing] Connected to Postgres & pgvector")

	// ========================================================================
	//  Websocket to gather data
	// ========================================================================
	// Initiate executor struct
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	// Start WebSocket Listener
	logger.Info(fmt.Sprintf("[Initializing] Connected to Binance Futures [%s @ %s]", Symbol, Interval))

	// Create a channel to keep main alive (or use a signal handler)
	doneC := make(chan struct{})

	errHandler := func(err error) {
		logger.Info(fmt.Sprintf("WebSocket Error: %v", err))
	}

	log.Println("[Initializing] Initializing websocket")
	wsHandler := func(event *futures.WsKlineEvent) {

		// We ONLY care when the candle is closed (IsFinal = true)
		if !event.Kline.IsFinal {
			return
		}

		// ---  HOT PATH: Candle Just Closed ---
		// event.Kline.IsFinal is True

		liveCandle := ai.InputData{
			Time:   event.Kline.StartTime / 1000,
			Open:   parse(event.Kline.Open),
			High:   parse(event.Kline.High),
			Low:    parse(event.Kline.Low),
			Close:  parse(event.Kline.Close),
			Volume: parse(event.Kline.Volume),
		}
		logger.Info("==================== START ====================")
		logger.Info(fmt.Sprintf("[Event] Candle Closed: %s | Price: %.4f\n",
			time.Unix(liveCandle.Time, 0).Format("15:04:05"),
			liveCandle.Close,
		))
		// 2. Fetch History via REST
		// We request Window + 5 to handle overlaps safely
		history, err := fetchRealHistory(binanceClient, Symbol, Interval, VectorWindow+5)
		if err != nil {
			logger.Info(fmt.Sprintf("API Error: %v", err))
			return
		}

		// 3. Safe Merge (Deduplication & Gap Check)
		cleanWindow, err := SafeMerge(history, liveCandle, VectorWindow, IntervalSecs)
		if err != nil {
			logger.Info(fmt.Sprintf("Data Integrity Skip: %v", err))
			return
		}

		// FIX: Pass feature.Embedding (Current) and matches (Historical)
		//filename := fmt.Sprintf("chart_%s.png", time.Now().Format("150405"))
		const fileProj string = "chart.png"
		if err != nil {
			logger.Info(fmt.Sprintf("Plot Error: %v", err))
		} else {
			logger.Info(fmt.Sprintf("Chart saved: %s", fileProj))
		}

		// filename_cancdle_chart := fmt.Sprintf("candle_chart_%s.png", time.Now().Format("150405"))
		const fileCandle string = "candle.png"
		err_candle_chart := plot.GenerateCandleChart(cleanWindow, fileCandle)
		if err != nil {
			logger.Info(fmt.Sprintf("Plot Error: %v", err_candle_chart))
		} else {
			logger.Info(fmt.Sprintf("Chart saved: %s", fileCandle))
		}

	}

	// Start the stream
	done, _, err := futures.WsKlineServe(Symbol, Interval, wsHandler, errHandler)
	if err != nil {
		log.Fatalf("Failed to connect WS: %v", err)
	}
	fmt.Println(err)

	<-done
	<-doneC
}

// IsTimeWindowOpen checks if we are close to :00 or :30
func IsTimeWindowOpen() bool {
	min := time.Now().Minute()

	// Window 1: Top of Hour (Catch 58, 59, 00, 01, 02, 03)
	if min >= 58 || min <= 3 {
		return true
	}

	// Window 2: Bottom of Hour (Catch 28, 29, 30, 31, 32, 33)
	if min >= 28 && min <= 33 {
		return true
	}

	return false
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
		va, _ := strconv.ParseFloat(k.Volume, 0)

		data[i] = ai.InputData{
			Time:   openTime,
			Open:   op, // <--- This was missing
			High:   hi, // <--- This was missing
			Low:    lo, // <--- This was missing
			Close:  cl,
			Volume: va,
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
