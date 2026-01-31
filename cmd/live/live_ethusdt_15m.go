package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/adshao/go-binance/v2/futures"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/ai"
	"time-series-rag-agent/internal/database"
	"time-series-rag-agent/internal/llm"
	"time-series-rag-agent/internal/notifier"
	"time-series-rag-agent/internal/plot"
	"time-series-rag-agent/internal/s3"
	"time-series-rag-agent/internal/sqs"
	"time-series-rag-agent/internal/trade"
	"time-series-rag-agent/pkg"
)

// --- Configuration ---
const (
	Symbol            = "ETHUSDT"
	Interval          = "15m"   // Binance string
	IntervalSecs      = 60 * 15 // 15m = 60 * 15 // Used for math checks (1m = 60s)
	VectorWindow      = 60      // N candles for the pattern
	top_k             = 18
	signalConfidence  = 30
	AviableTradeRatio = 0.9
)

func main() {
	logger := pkg.SetupLogger()

	basicContext, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	logger.Info(fmt.Sprintf("==== Proceed trading symbol: %s | interval: %s | TopK: %d ====", Symbol, Interval, top_k))
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
		log.Fatalf("Database Connection Failed: %v", err)
	}
	defer pg.Close()

	discord := notifier.NewDiscordClient(
		cfg.Discord.DISCORD_ALERT_WEBHOOK_URL,
		cfg.Discord.DISCORD_NOTIFY_WEBHOOK_URL,
	)
	log.Println("[Initializing] Connected to Postgres & pgvector")

	// ========================================================================
	//  Websocket to gather data
	// ========================================================================
	agent := ai.NewPatternAI(Symbol, Interval, "v1", VectorWindow)
	// Initiate executor struct
	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)
	executor := trade.NewExecutor(
		binanceClient,
		Symbol,
		cfg.Agent.AviableTradeRatio,
		cfg.Agent.Leverage,
		cfg.Agent.SLPercentage,
		cfg.Agent.TPPercentage,
		*logger,
	)
	if err := executor.SetLeverage(basicContext, executor.Leverage); err != nil {
		logger.Info(fmt.Sprintln("Error syncing leverage:", err))
		logger.Info(fmt.Sprintln("Leverage:", executor.Leverage))
		return
	}

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
		start := time.Now()

		liveCandle := ai.InputData{
			Time:  event.Kline.StartTime / 1000,
			Open:  parse(event.Kline.Open),
			High:  parse(event.Kline.High),
			Low:   parse(event.Kline.Low),
			Close: parse(event.Kline.Close),
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

		// 4. Calculate Features
		feature := agent.CalculateFeatures(cleanWindow)
		if feature == nil {
			return
		}
		logger.Info(fmt.Sprintf("[Embedding] Feature Ready in %v | Embedding Size: %d\n", time.Since(start), len(feature.Embedding)))

		go func(feat *ai.PatternFeature, window []ai.InputData) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// A. Calculate Labels (The "Truth" about the past)
			labels := agent.CalculateLabels(window)

			// B. Ingest (Insert T, Update T-n)
			err := pg.IngestPattern(ctx, feat, labels)
			if err != nil {
				logger.Info(fmt.Sprintf("Ingestion Failed: %v", err))
			} else {
				logger.Info(fmt.Sprintf("[Ingest] Saved T (%s) & Updated %d Past Labels",
					feat.Time.Format("15:04"), len(labels)))
			}
		}(feature, cleanWindow) // Pass copies/pointers safely

		hasPos, _, _, err := executor.HasOpenPosition(context.Background())
		if err != nil {
			logger.Info(fmt.Sprintf("Failed to check position: %v", err))
			return // Safer to do nothing if API fails
		}

		if hasPos {
			logger.Info(fmt.Sprintf("[Contract] Skip... In Trade (%s). Skipping Analysis.", Symbol))
			return // <--- NOW SAFE: Ingestion already started above!
		}

		matches, err := pg.SearchPatterns(context.Background(), feature.Embedding, top_k, Symbol)
		if len(matches) > 0 {

			// ---------------------------------------------------------
			// TIME GUARD
			// We found matches (good for BI), but we only act on :00 and :30
			// to save LLM costs and reduce market noise.
			// ---------------------------------------------------------
			// if !IsTimeWindowOpen() {
			// 	logger.Info(fmt.Sprintf("[TimeGuard] Time %s is outside strategy window (:00/:30). Skipping LLM & Trade.", time.Now().Format("15:04")))
			// 	// We return (or 'continue' if inside a loop) to finish this cycle
			// 	// without calling the LLM.
			// 	return
			// }

			logger.Info(fmt.Sprintf("[Embedding] Found %d matches. Visualizing alignment...", len(matches)))

			// FIX: Pass feature.Embedding (Current) and matches (Historical)
			//filename := fmt.Sprintf("chart_%s.png", time.Now().Format("150405"))
			const fileProj string = "chart.png"

			err := plot.GeneratePredictionChart(feature.Embedding, matches, fileProj)

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

			llmClient := llm.NewLLMService(cfg.OpenRouter.ApiKey)
			sysMsg, usrMsg, b64A, b64B, err := llmClient.GenerateTradingPrompt(
				time.Now().Format("15:04:05"),
				matches,
				fileProj,   // Chart A (Macro)
				fileCandle, // Chart B (Micro)
			)
			if err != nil {
				logger.Info(fmt.Sprintf("Prompt Error: %v", err))
				return
			}

			discord.NotifyPipeline(
				fmt.Sprintf("Analyzing %s pattern...", Symbol),
				fileProj,
			)

			signal, err := llmClient.GenerateSignal(context.Background(), sysMsg, usrMsg, b64A, b64B)
			if err != nil {
				logger.Info(fmt.Sprintf("LLM Error: %v", err))
				return
			}

			tradeMsg := fmt.Sprintf(
				"**SIDE:** %s\n**CONFIDENCE:** %d%%\n**REASON:** %s",
				signal.Signal, signal.Confidence, signal.Synthesis,
			)

			if signal.Confidence >= signalConfidence {

				if signal.Signal == "SHORT" || signal.Signal == "LONG" {
					priceToOpen, err_conv := strconv.ParseFloat(event.Kline.Close, 64)
					if err_conv != nil {
						logger.Info(fmt.Sprintf("Trade failed: %v", err_conv))
					}

					tradeCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					err = executor.PlaceTrade(tradeCtx, signal.Signal, priceToOpen)
					if err != nil {
						logger.Info(fmt.Sprintln(err))
					}
				}

			} else {
				tradeMsg = fmt.Sprintf("%s\n**NOTE:** Signal confidence below threshold (%d%% < %d%%). No trade executed.",
					tradeMsg, signal.Confidence, signalConfidence)
				logger.Info("[Signal] Confidence below threshold. No trade executed.")
			}

			logsContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			candleKey, err := s3.UploadImageToS3(logsContext, "candle.png")
			chartKey, err := s3.UploadImageToS3(logsContext, "chart.png")

			messageQue := map[string]string{
				"signal":      signal.Signal,
				"reason":      signal.Synthesis,
				"candleKey":   candleKey, // e.g., "image/candle/2026/01/31/..."
				"chartKey":    chartKey,
				"symbol":      Symbol,
				"recorded_at": fmt.Sprint(event.Kline.StartTime / 1000),
			}

			messageQueJsonData, err := json.Marshal(messageQue)
			if err != nil {
				fmt.Println("Error marshaling:", err)
				return
			}
			messageQueString := string(messageQueJsonData)

			// Now call your SQS function
			sqs.PutTradingLog(messageQueString)

			// Sending Trade Alert (Candle Chart)
			discord.NotifyPipeline(tradeMsg, fileCandle)

			discord.NotifyPipeline(
				fmt.Sprintln("**SetupTeir:** ", signal.SetupTeir),
				"",
			)
			discord.NotifyPipeline(
				fmt.Sprintln("**VisualQuality:** ", signal.VisualQuality),
				"",
			)
			discord.NotifyPipeline(
				fmt.Sprintln("**ChartBTrigger:** ", signal.ChartBTrigger),
				"",
			)

			// 3. Act
			logger.Info(fmt.Sprintf("[LLM] SIGNAL: %s (Conf: %d%%)", signal.Signal, signal.Confidence))
			logger.Info(fmt.Sprintf("[LLM] Reasoning: %s", signal.Synthesis))
		}
	}

	// Start the stream
	done, _, err := futures.WsKlineServe(Symbol, Interval, wsHandler, errHandler)
	if err != nil {
		log.Fatalf("Failed to connect WS: %v", err)
	}

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
