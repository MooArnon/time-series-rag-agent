package main

import (
	"time-series-rag-agent/internal/pipeline"
	"time-series-rag-agent/pkg/logger"
)

const (
	symbol     = "BTCUSDT"
	vectorSize = 30
)

func main() {
	logger := logger.SetupLogger()

	err1Minute := pipeline.RestIngestVectorFlow(logger, symbol, "1m", vectorSize)
	if err1Minute != nil {
		logger.Error("[Error] Tested error")
	}

	err1Hour := pipeline.RestIngestVectorFlow(logger, symbol, "1h", vectorSize)
	if err1Hour != nil {
		logger.Error("[Error] Tested error")
	}
}
