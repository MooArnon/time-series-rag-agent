package main

import (
	"context"
	"fmt"
	"os"
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
)

const (
	SYMBOL        = "BTCUSDT"
	INTERVAL      = "1h"
	VECTOR_WINDOW = 30
	FETCH_LIMIT   = 2000
	DAY_LOOK_BACK = 2391
)

func main() {
	logger := logger.SetupLogger()
	ctx := context.Background()
	if err := pipeline.NewBackfillPipeline(ctx, logger, SYMBOL, INTERVAL, FETCH_LIMIT, VECTOR_WINDOW, DAY_LOOK_BACK); err != nil {
		logger.Error(fmt.Sprintf("Backfill failed: %v", err))
		os.Exit(1)
	}
}
