package embedding

import "math"

// PlanckConstant is used as a numerical stability epsilon.
const PlanckConstant = 6.62607015e-34

// CalculateLogReturn returns log returns from a slice of close prices.
// Output length = len(closes) - 1.
func CalculateLogReturn(closes []float64) []float64 {
	if len(closes) < 2 {
		return []float64{}
	}
	res := make([]float64, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		curr := math.Log(closes[i] + PlanckConstant)
		prev := math.Log(closes[i-1] + PlanckConstant)
		res[i-1] = curr - prev
	}
	return res
}

// CalculateZScore normalizes a slice to zero mean and unit variance.
func CalculateZScore(data []float64) []float64 {
	if len(data) == 0 {
		return []float64{}
	}

	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))

	sqDiffSum := 0.0
	for _, v := range data {
		sqDiffSum += math.Pow(v-mean, 2)
	}
	std := math.Sqrt(sqDiffSum / float64(len(data)))

	res := make([]float64, len(data))
	for i, v := range data {
		res[i] = (v - mean) / (std + PlanckConstant)
	}
	return res
}

// CalculateSlope computes the linear regression slope of normalized prices.
// Equivalent to np.polyfit(x, y_norm, 1)[0].
func CalculateSlope(prices []float64) float64 {
	n := float64(len(prices))
	if n < 2 {
		return 0.0
	}

	startVal := prices[0]
	if startVal == 0 {
		startVal = 1e-9
	}

	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i, p := range prices {
		x := float64(i)
		yNorm := (p - startVal) / startVal
		sumX += x
		sumY += yNorm
		sumXY += x * yNorm
		sumX2 += x * x
	}

	numerator := (n * sumXY) - (sumX * sumY)
	denominator := (n * sumX2) - (sumX * sumX)
	if denominator == 0 {
		return 0.0
	}
	return numerator / denominator
}
