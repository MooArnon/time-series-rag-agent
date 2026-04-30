package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
)

func main() {
	symbol := flag.String("symbol", "BTCUSDT", "trading pair symbol (e.g. BTCUSDT)")
	interval := flag.String("interval", "15m", "candle interval (e.g. 15m, 1h)")
	vectorWindow := flag.Int("vector-window", 30, "embedding vector window size")
	fetchLimit := flag.Int("fetch-limit", 2000, "max candles per REST request")
	dayLookback := flag.Int("days", 1000, "number of days to look back")
	flag.Parse()

	logger := logger.SetupLogger()
	ctx := context.Background()

	logger.Info(fmt.Sprintf("[Backfill] symbol=%s interval=%s days=%d", *symbol, *interval, *dayLookback))

	if err := pipeline.NewBackfillPipeline(ctx, logger, *symbol, *interval, *fetchLimit, *vectorWindow, *dayLookback); err != nil {
		logger.Error(fmt.Sprintf("Backfill failed: %v", err))
		os.Exit(1)
	}
}
