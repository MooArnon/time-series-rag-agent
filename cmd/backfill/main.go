package main

import (
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
)

const (
	SYMBOL        = "BTCUSDT"
	INTERVAL      = "15m"
	VECTOR_WINDOW = 30
	FETCH_LIMIT   = 2000
	DAY_LOOK_BACK = 365
)

func main() {
	logger := logger.SetupLogger()
	pipeline.NewBackfillPipeline(*logger, SYMBOL, INTERVAL, FETCH_LIMIT, VECTOR_WINDOW, DAY_LOOK_BACK)
}
