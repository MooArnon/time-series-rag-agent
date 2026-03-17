package pipeline

import (
	"log/slog"

	"time-series-rag-agent/internal/embedding"
	"time-series-rag-agent/internal/exchange"
)

func NewEmbeddingPipeline(
	logger slog.Logger,
	wsCandle []exchange.WsCandle,
	restCandle []exchange.RestCandle,
	vectorSize int,
	symbol string,
	interval string,
) (*embedding.PatternFeature, []embedding.LabelUpdate, []exchange.WsRestCandle) {
	logger.Info("[EmbeddingPipeline] Starting Embedding Pipeline")
	// -- Features -- //
	fc := embedding.NewFeatureCalculator(symbol, interval, vectorSize)
	wsRestCandle := embedding.MergeCandles(wsCandle, restCandle)

	featureCalculateCandle := wsRestCandle[:vectorSize+1]
	feature := fc.Calculate(featureCalculateCandle)

	// -- Labels -- //
	lc := embedding.NewLabelCalculator()
	label := lc.CalculateFromHistory(featureCalculateCandle)

	return feature, label, wsRestCandle
}

func NewBackfillEmbeddingPipeline(
	logger slog.Logger,
	restCandles []exchange.RestCandle,
	symbol string,
	interval string,
	vectorWindow int,
) ([]embedding.PatternFeature, []embedding.LabelUpdate) {
	logger.Info("[EmbeddingPipeline] Starting Backfill Pipeline")

	fc := embedding.NewFeatureCalculator(symbol, interval, vectorWindow)
	lc := embedding.NewLabelCalculator()

	// Convert once
	inputData := make([]exchange.WsRestCandle, len(restCandles))
	for i, c := range restCandles {
		inputData[i] = exchange.WsRestCandle{
			Time: c.Time, Open: c.Open, High: c.High,
			Low: c.Low, Close: c.Close, Volume: c.Volume,
		}
	}

	var features []embedding.PatternFeature
	var labels []embedding.LabelUpdate

	for i := vectorWindow; i < len(inputData); i++ {
		feature := fc.Calculate(inputData[i-vectorWindow : i+1])
		if feature == nil {
			continue
		}
		features = append(features, *feature)
		labels = append(labels, lc.CalculateLookahead(inputData, i, feature.Time.Unix())...)
	}

	return features, labels
}
