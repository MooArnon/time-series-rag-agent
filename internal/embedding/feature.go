package embedding

import (
	"fmt"
	"time"
	"time-series-rag-agent/internal/exchange"
)

// FeatureCalculatorI allows mocking in tests.
type FeatureCalculatorI interface {
	Calculate(history []exchange.WsRestCandle) *PatternFeature
}

// FeatureCalculator computes embeddings from a rolling window of candles.
type FeatureCalculator struct {
	Symbol       string
	Interval     string
	VectorWindow int
}

func NewFeatureCalculator(symbol, interval string, vectorWindow int) *FeatureCalculator {
	return &FeatureCalculator{
		Symbol:       symbol,
		Interval:     interval,
		VectorWindow: vectorWindow,
	}
}

// Calculate returns a PatternFeature from the last (VectorWindow+1) candles.
// Returns nil if history is too short.
func (f *FeatureCalculator) Calculate(history []exchange.WsRestCandle) *PatternFeature {
	reqLen := f.VectorWindow + 1
	if len(history) < reqLen {
		return nil
	}

	window := history[len(history)-reqLen:]

	closes := make([]float64, len(window))
	for i, d := range window {
		closes[i] = d.Close
	}

	logReturns := CalculateLogReturn(closes)
	embedding := CalculateZScore(logReturns)
	lastCandle := window[len(window)-1]

	return &PatternFeature{
		Time:       time.Unix(lastCandle.Time, 0),
		Symbol:     f.Symbol,
		Interval:   f.Interval,
		Embedding:  embedding,
		ClosePrice: lastCandle.Close,
	}
}

func (f *FeatureCalculator) CalculateRest(history []exchange.RestCandle) *PatternFeature {
	reqLen := f.VectorWindow + 1
	if len(history) < reqLen {
		return nil
	}

	window := history[len(history)-reqLen:]

	closes := make([]float64, len(window))
	for i, d := range window {
		closes[i] = d.Close
	}

	fmt.Println("closes: ", closes)

	logReturns := CalculateLogReturn(closes)
	embedding := CalculateZScore(logReturns)
	lastCandle := window[len(window)-1]

	return &PatternFeature{
		Time:       time.Unix(lastCandle.Time, 0),
		Symbol:     f.Symbol,
		Interval:   f.Interval,
		Embedding:  embedding,
		ClosePrice: lastCandle.Close,
	}
}

func (f *FeatureCalculator) BulkCalculate(history []exchange.RestCandle) *PatternFeature {
	reqLen := f.VectorWindow + 1
	if len(history) < reqLen {
		return nil
	}

	window := history[len(history)-reqLen:]

	closes := make([]float64, len(window))
	for i, d := range window {
		closes[i] = d.Close
	}

	logReturns := CalculateLogReturn(closes)
	embedding := CalculateZScore(logReturns)
	lastCandle := window[len(window)-1]

	return &PatternFeature{
		Time:       time.Unix(lastCandle.Time, 0),
		Symbol:     f.Symbol,
		Interval:   f.Interval,
		Embedding:  embedding,
		ClosePrice: lastCandle.Close,
	}
}
