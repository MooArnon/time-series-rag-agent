package ai

import (
	"math"
	"testing"
	"time"
)

// Test CalculateSlope (formerly GetSlope)
func TestCalculateSlope(t *testing.T) {
	// 1. Flat line (Slope = 0)
	flat := []float64{10, 10, 10, 10}
	slope := CalculateSlope(flat)
	if slope != 0 {
		t.Errorf("Expected slope 0, got %f", slope)
	}

	// 2. Linear increase (Slope should be positive)
	// y = x (normalized against start value)
	rising := []float64{10, 11, 12, 13}
	slopeRising := CalculateSlope(rising)
	if slopeRising <= 0 {
		t.Errorf("Expected positive slope, got %f", slopeRising)
	}

	// 3. Linear decrease
	falling := []float64{10, 9, 8, 7}
	slopeFalling := CalculateSlope(falling)
	if slopeFalling >= 0 {
		t.Errorf("Expected negative slope, got %f", slopeFalling)
	}
}

// Test CalculateZScore
func TestCalculateZScore(t *testing.T) {
	data := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	zscores := CalculateZScore(data)

	if len(zscores) != len(data) {
		t.Fatalf("Expected output length %d, got %d", len(data), len(zscores))
	}

	// Sum of Z-scores should be approximately 0
	sum := 0.0
	for _, z := range zscores {
		sum += z
	}

	if math.Abs(sum) > 1e-9 {
		t.Errorf("Sum of Z-scores should be ~0, got %f", sum)
	}
}

// Test PatternAI Features (formerly CalcFeatures)
func TestCalculateFeatures(t *testing.T) {
	// Setup
	ai := NewPatternAI("BTCUSDT", "1h", "v1", 5) // Window = 5
	
	// Create dummy history (need window+1 = 6 items)
	history := []InputData{
		{Time: 1000, Close: 100.0},
		{Time: 1001, Close: 102.0},
		{Time: 1002, Close: 101.0},
		{Time: 1003, Close: 103.0},
		{Time: 1004, Close: 104.0},
		{Time: 1005, Close: 105.0}, // Last candle
	}

	// Execute
	feature := ai.CalculateFeatures(history)

	// Assertions
	if feature == nil {
		t.Fatal("Expected feature to be generated, got nil")
	}

	if feature.Symbol != "BTCUSDT" {
		t.Errorf("Expected symbol BTCUSDT, got %s", feature.Symbol)
	}

	if len(feature.Embedding) != 5 {
		t.Errorf("Expected embedding length 5, got %d", len(feature.Embedding))
	}

	// Check time conversion (int64 -> time.Time)
	expectedTime := time.Unix(1005, 0)
	if !feature.Time.Equal(expectedTime) {
		t.Errorf("Expected time %v, got %v", expectedTime, feature.Time)
	}
}

// Test Bulk Calculation
func TestCalculateBulkData(t *testing.T) {
	ai := NewPatternAI("ETHUSDT", "15m", "v1", 3) // Window = 3
	
	// Create enough history for at least 2 windows
	// Indices: 0, 1, 2, 3 (First Window), 4 (Second Window) -> Length 5
	history := []InputData{
		{Time: 100, Close: 10.0},
		{Time: 200, Close: 11.0},
		{Time: 300, Close: 12.0},
		{Time: 400, Close: 13.0}, // End of first valid window (indices 0-3)
		{Time: 500, Close: 14.0}, // End of second valid window (indices 1-4)
		{Time: 600, Close: 15.0}, // Future data for labels
	}

	results := ai.CalculateBulkData(history)

	if len(results) == 0 {
		t.Fatal("Expected bulk results, got 0")
	}

	// Verify the first result
	firstRes := results[0]
	if firstRes.Features.ClosePrice != 13.0 {
		t.Errorf("Expected first feature close 13.0, got %f", firstRes.Features.ClosePrice)
	}

	// Check if labels were generated
	hasReturnLabel := false
	for _, lbl := range firstRes.Labels {
		if lbl.Column == "next_return" {
			hasReturnLabel = true
			// Next return for close 13.0 (at 400) is close 14.0 (at 500)
			// (14-13)/13 = 1/13 ≈ 0.0769
			if lbl.Value < 0.07 || lbl.Value > 0.08 {
				t.Errorf("Unexpected return label value: %f", lbl.Value)
			}
		}
	}

	if !hasReturnLabel {
		t.Error("Missing 'next_return' label in bulk results")
	}
}
