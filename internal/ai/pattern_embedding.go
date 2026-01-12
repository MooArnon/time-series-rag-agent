package ai

import (
	"time"
)

// Fixed: Used 'type' keyword for struct definition
type PatternAI struct {
	Symbol       string
	Interval     string
	Model        string
	VectorWindow int
}

// Fixed: Added commas between parameters
func NewPatternAI(
	Symbol string,
	Interval string,
	Model string,
	VectorWindow int,
) *PatternAI {
	return &PatternAI{
		Symbol:       Symbol,
		Interval:     Interval,
		Model:        Model,
		VectorWindow: VectorWindow, // Fixed: Added commas
	}
}

// Fixed: Used 'type' keyword
type InputData struct {
	Time  int64
	Open  float64
	High  float64
	Low   float64
	Close float64
}

// Fixed: Added struct definition for LabelUpdate (inferred from usage)
type LabelUpdate struct {
	TargetTime int64
	Column     string
	Value      float64
}

// Fixed: Added alias for FeatureResult to match usage in BulkResult
type FeatureResult = PatternFeature

func (p *PatternAI) CalculateFeatures(history []InputData) *PatternFeature {
	reqLen := p.VectorWindow + 1

	// Not enough data
	if len(history) < reqLen {
		return nil
	}

	// Slice the last N+1
	window := history[len(history)-reqLen:]

	// Extract close price
	closes := make([]float64, len(window))
	for i, d := range window {
		// Fixed: Use '=' for assignment to existing slice index, not ':='
		closes[i] = d.Close
	}

	// Log return
	LogReturn := CalculateLogReturn(closes)

	// Normalize (Embedding)
	embedding := CalculateZScore(LogReturn)

	lastCandle := window[len(window)-1]

	return &PatternFeature{
		Time:       time.Unix(lastCandle.Time, 0), // Fixed: Convert int64 to time.Time
		Symbol:     p.Symbol,
		Interval:   p.Interval,
		Embedding:  embedding,
		ClosePrice: lastCandle.Close, // Fixed: Added commas
	}
}

// CalculateLabels: Generates targets for PAST candles based on current data
// Equivalent to: self.calculate_labels(data)
func (p *PatternAI) CalculateLabels(history []InputData) []LabelUpdate {
	updates := []LabelUpdate{}
	n := len(history)
	if n < 2 {
		return updates
	}

	// --- Label A: Next Return (For T-1) ---
	// Logic: We look at the 2nd to last candle (T-1) and calculate return using Last (T)
	prevIdx := n - 2
	currIdx := n - 1

	prevClose := history[prevIdx].Close
	currClose := history[currIdx].Close

	if prevClose != 0 {
		ret := (currClose - prevClose) / prevClose
		updates = append(updates, LabelUpdate{
			TargetTime: history[prevIdx].Time,
			Column:     "next_return",
			Value:      ret,
		})
	}

	// --- Label B: Slope 3 (For T-3) ---
	// We need the candle at T-3 to be the "start", followed by 3 future candles
	// Index Logic: Target is n-4. Future is n-3, n-2, n-1.
	targetIdx3 := n - 4
	if targetIdx3 >= 0 {
		// Slice from n-3 to end (which is 3 candles: n-3, n-2, n-1)
		futurePrices := []float64{
			history[n-3].Close,
			history[n-2].Close,
			history[n-1].Close,
		}
		// Fixed: Renamed GetSlope -> CalculateSlope to match previous file
		slope := CalculateSlope(futurePrices)
		updates = append(updates, LabelUpdate{
			TargetTime: history[targetIdx3].Time,
			Column:     "next_slope_3",
			Value:      slope,
		})
	}

	// --- Label C: Slope 5 (For T-5) ---
	targetIdx5 := n - 6
	if targetIdx5 >= 0 {
		// Slice from n-5 to end (5 candles)
		futurePrices := []float64{}
		for i := targetIdx5 + 1; i < n; i++ {
			futurePrices = append(futurePrices, history[i].Close)
		}

		slope := CalculateSlope(futurePrices)
		updates = append(updates, LabelUpdate{
			TargetTime: history[targetIdx5].Time,
			Column:     "next_slope_5",
			Value:      slope,
		})
	}

	return updates
}

type BulkResult struct {
	Features FeatureResult
	Labels   []LabelUpdate
}

func (p *PatternAI) CalculateBulkData(fullHistory []InputData) []BulkResult {
	results := []BulkResult{}

	// Loop through history
	// Start loop where we have enough data for a window
	// If Window=60, we need index 60 (61st item) to look back 60 returns
	startIndex := p.VectorWindow

	for i := startIndex; i < len(fullHistory); i++ {
		// --- 1. Calculate Feature for this row (i) ---

		// Create a window slice ending at i
		// (Go slice exclude 'end', so we normally do i+1,
		// but since we pass slice to CalcFeatures, let's construct it manually for clarity)

		// We need the previous 'VectorWindow + 1' items to get 'VectorWindow' returns
		windowStart := i - p.VectorWindow
		// slice: [start : current+1]
		currentSlice := fullHistory[windowStart : i+1]

		feature := p.CalculateFeatures(currentSlice)
		if feature == nil {
			continue
		}

		// --- 2. Calculate Labels for this row (i) ---
		// Note: In Python bulk code, you looked AHEAD.
		// "At time T, what is the return at T+1?"

		labels := []LabelUpdate{}

		// Label A: Next Return (Target for THIS row)
		if i+1 < len(fullHistory) {
			currClose := fullHistory[i].Close
			nextClose := fullHistory[i+1].Close
			if currClose != 0 {
				ret := (nextClose - currClose) / currClose
				labels = append(labels, LabelUpdate{
					TargetTime: feature.Time.Unix(), // Fixed: Time.Time -> int64
					Column:     "next_return",
					Value:      ret,
				})
			}
		}

		// Label B: Slope 3 (Look ahead 3 candles)
		if i+3 < len(fullHistory) {
			futurePrices := []float64{}
			for k := 1; k <= 3; k++ {
				futurePrices = append(futurePrices, fullHistory[i+k].Close)
			}
			slope := CalculateSlope(futurePrices)
			labels = append(labels, LabelUpdate{
				TargetTime: feature.Time.Unix(),
				Column:     "next_slope_3",
				Value:      slope,
			})
		}

		// Label C: Slope 5 (Look ahead 5 candles)
		if i+5 < len(fullHistory) {
			futurePrices := []float64{}
			for k := 1; k <= 5; k++ {
				futurePrices = append(futurePrices, fullHistory[i+k].Close)
			}
			slope := CalculateSlope(futurePrices)
			labels = append(labels, LabelUpdate{
				TargetTime: feature.Time.Unix(),
				Column:     "next_slope_5",
				Value:      slope,
			})
		}

		results = append(results, BulkResult{
			Features: *feature,
			Labels:   labels,
		})
	}

	return results
}
