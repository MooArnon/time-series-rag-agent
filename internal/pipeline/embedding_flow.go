package pipeline

import (
	"fmt"
	"log/slog"

	"time-series-rag-agent/internal/embedding"
	"time-series-rag-agent/internal/exchange"
)

func NewEmbeddingPipeline(logger slog.Logger, wsCandle []exchange.WsCandle, restCandle []exchange.RestCandle, symbol string, interval string) MarketPattern {
	logger.Info("[EmbeddingPipeline] Starting Embedding Pipeline")
	candle := embedding.MergeCandles(wsCandle, restCandle)
	logger.Info(fmt.Sprintln("[EmbeddingPipeline] candle: ", candle))

	fc := embedding.NewFeatureCalculator(symbol, interval, len(candle))
	feature := fc.Calculate(candle)
	logger.Info(fmt.Sprint("[EmbeddingPipeline] ", feature))

	return MarketPattern{}
}
