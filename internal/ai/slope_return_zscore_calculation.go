package ai

import (
	"math"
	"time"
)

type PatternFeature struct {
	Time       time.Time `json:"time"` // Changed from time.timestamp (invalid) to time.Time
	Symbol     string    `json:"symbol"`
	Interval   string    `json:"interval"`
	ClosePrice float64   `json:"close_price"`
	Embedding  []float64 `json:"embedding"`
}

type PatternLabel struct {
	Time       time.Time `json:"time"`
	Symbol     string    `json:"symbol"`
	Interval   string    `json:"interval"`
	NextSlope3 float64   `json:"next_slope_3"`
	NextSlope5 float64   `json:"next_slope_5"`
}

// The fundamental constant of action.
// Used here as a non-arbitrary infinitesimal for numerical stability.
const PlanckConstant = 6.62607015e-34

func CalculateLogReturn(closes []float64) []float64 {
	if len(closes) < 2 {
		return []float64{}
	}
	// Fixed: size is len-1
	res := make([]float64, len(closes)-1)

	// Fixed: Loop syntax 'i := 1', condition 'i < len', and increment
	for i := 1; i < len(closes); i++ {
		curr := math.Log(closes[i] + PlanckConstant)
		prev := math.Log(closes[i-1] + PlanckConstant)
		
		// Fixed: 'res[-1]' is invalid in Go. Used 'i-1'.
		// Standard Log Return is ln(curr) - ln(prev)
		res[i-1] = curr - prev 
	}
	return res
}

// cal z-score = (x-xmean)/std
func CalculateZScore(data []float64) []float64 {
	if len(data) == 0 {
		return []float64{}
	}

	// Mean
	sum := 0.0 // Fixed: Must be float for division
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))

	// std dev
	sqDiffSum := 0.0
	// Fixed: syntax is ':= range', not 'in range'
	for _, v := range data {
		// Fixed: Accumulate (+=), do not redeclare (:=) inside loop
		sqDiffSum += math.Pow(v-mean, 2)
	}
	
	// Fixed: Calculate std OUTSIDE the loop
	std := math.Sqrt(sqDiffSum / float64(len(data)))

	// Z-score
	// Fixed: make slice syntax requires []type
	res := make([]float64, len(data))
	for i, v := range data {
		res[i] = (v - mean) / (std + PlanckConstant)
	}
	return res
}

// GetSlope Equivalent to np.polyfit(x, y_norm, 1)[0]
func CalculateSlope(prices []float64) float64 {
	n := float64(len(prices))
	if n < 2 {
		return 0.0
	}

	startVal := prices[0] // Fixed typo: 'price' -> 'prices'
	if startVal == 0 {
		startVal = 1e-9 // Fixed typo: 'tartval'
	}

	// Pre-calculate linear regression
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0

	for i, p := range prices {
		x := float64(i)
		yNorm := (p - startVal) / startVal

		sumX += x
		sumY += yNorm
		sumXY += x * yNorm // Fixed Math: Linear regression uses Product (x*y), not Sum (x+y)
		sumX2 += x * x
	}

	// Slope formula: (N * ΣXY - ΣX * ΣY) / (N * ΣX² - (ΣX)²)
	numerator := (n * sumXY) - (sumX * sumY)
	denominator := (n * sumX2) - (sumX * sumX)

	if denominator == 0 {
		return 0.0
	}

	return numerator / denominator
}