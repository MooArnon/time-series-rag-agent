package embedding

import (
	"math"
	"time"
)

type InputData struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type PatternFeature struct {
	Time       time.Time `json:"time"`
	Symbol     string    `json:"symbol"`
	Interval   string    `json:"interval"`
	ClosePrice float64   `json:"close_price"`
	Embedding  []float64 `json:"embedding"`
}

type PatternLabel struct {
	Time       time.Time `json:"time"`
	Symbol     string    `json:"symbol"`
	Interval   string    `json:"interval"`
	NextReturn float64   `json:"next_return"`
	NextSlope3 float64   `json:"next_slope_3"`
	NextSlope5 float64   `json:"next_slope_5"`
	Embedding  []float64
	Distance   float64
}

type LabelUpdate struct {
	TargetTime int64
	Column     string
	Value      float64
}

type BulkResult struct {
	Feature PatternFeature
	Labels  []LabelUpdate
}

// SimilarityPct converts cosine distance to similarity percentage
func (p PatternLabel) SimilarityPct() float64 {
	return math.Max(0, (1.0-p.Distance)*100)
}

// TrendOutcome returns "UP" or "DOWN" based on NextSlope3
func (p PatternLabel) TrendOutcome() string {
	if p.NextSlope3 < 0 {
		return "DOWN"
	}
	return "UP"
}
