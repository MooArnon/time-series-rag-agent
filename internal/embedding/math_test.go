package embedding

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- CalculateLogReturn ---

func TestCalculateLogReturn_NormalInput(t *testing.T) {
	// Arrange
	closes := []float64{100.0, 110.0, 121.0}

	// Act
	result := CalculateLogReturn(closes)

	// Assert
	assert.Len(t, result, 2)
	assert.InDelta(t, math.Log(110.0)-math.Log(100.0), result[0], 1e-9)
	assert.InDelta(t, math.Log(121.0)-math.Log(110.0), result[1], 1e-9)
}

func TestCalculateLogReturn_TwoElements_ReturnsSingleValue(t *testing.T) {
	// Arrange
	closes := []float64{100.0, 200.0}

	// Act
	result := CalculateLogReturn(closes)

	// Assert
	assert.Len(t, result, 1)
	assert.InDelta(t, math.Log(2.0), result[0], 1e-9)
}

func TestCalculateLogReturn_SingleElement_ReturnsEmpty(t *testing.T) {
	// Arrange
	closes := []float64{100.0}

	// Act
	result := CalculateLogReturn(closes)

	// Assert
	assert.Empty(t, result)
}

func TestCalculateLogReturn_Empty_ReturnsEmpty(t *testing.T) {
	// Arrange
	closes := []float64{}

	// Act
	result := CalculateLogReturn(closes)

	// Assert
	assert.Empty(t, result)
}

func TestCalculateLogReturn_FlatPrices_ReturnsNearZero(t *testing.T) {
	// Arrange
	closes := []float64{100.0, 100.0, 100.0}

	// Act
	result := CalculateLogReturn(closes)

	// Assert
	assert.Len(t, result, 2)
	assert.InDelta(t, 0.0, result[0], 1e-9)
	assert.InDelta(t, 0.0, result[1], 1e-9)
}

// --- CalculateZScore ---

func TestCalculateZScore_NormalInput_MeanNearZero(t *testing.T) {
	// Arrange
	data := []float64{10.0, 20.0, 30.0, 40.0, 50.0}

	// Act
	result := CalculateZScore(data)

	// Assert
	assert.Len(t, result, 5)

	sum := 0.0
	for _, v := range result {
		sum += v
	}
	assert.InDelta(t, 0.0, sum/float64(len(result)), 1e-9)
}

func TestCalculateZScore_NormalInput_StdNearOne(t *testing.T) {
	// Arrange
	data := []float64{10.0, 20.0, 30.0, 40.0, 50.0}

	// Act
	result := CalculateZScore(data)

	// Assert
	mean := 0.0
	for _, v := range result {
		mean += v
	}
	mean /= float64(len(result))

	sqSum := 0.0
	for _, v := range result {
		sqSum += math.Pow(v-mean, 2)
	}
	std := math.Sqrt(sqSum / float64(len(result)))

	assert.InDelta(t, 1.0, std, 1e-6)
}

func TestCalculateZScore_AllSameValues_ReturnsNearZero(t *testing.T) {
	// Arrange
	data := []float64{5.0, 5.0, 5.0, 5.0}

	// Act
	result := CalculateZScore(data)

	// Assert
	for _, v := range result {
		assert.InDelta(t, 0.0, v, 1e-6)
	}
}

func TestCalculateZScore_Empty_ReturnsEmpty(t *testing.T) {
	// Arrange
	data := []float64{}

	// Act
	result := CalculateZScore(data)

	// Assert
	assert.Empty(t, result)
}

func TestCalculateZScore_OutputLengthMatchesInput(t *testing.T) {
	// Arrange
	data := []float64{1.0, 2.0, 3.0}

	// Act
	result := CalculateZScore(data)

	// Assert
	assert.Len(t, result, len(data))
}

// --- CalculateSlope ---

func TestCalculateSlope_IncreasingPrices_PositiveSlope(t *testing.T) {
	// Arrange
	prices := []float64{100.0, 110.0, 120.0, 130.0}

	// Act
	result := CalculateSlope(prices)

	// Assert
	assert.Greater(t, result, 0.0)
}

func TestCalculateSlope_DecreasingPrices_NegativeSlope(t *testing.T) {
	// Arrange
	prices := []float64{130.0, 120.0, 110.0, 100.0}

	// Act
	result := CalculateSlope(prices)

	// Assert
	assert.Less(t, result, 0.0)
}

func TestCalculateSlope_FlatPrices_NearZeroSlope(t *testing.T) {
	// Arrange
	prices := []float64{100.0, 100.0, 100.0, 100.0}

	// Act
	result := CalculateSlope(prices)

	// Assert
	assert.InDelta(t, 0.0, result, 1e-9)
}

func TestCalculateSlope_SingleElement_ReturnsZero(t *testing.T) {
	// Arrange
	prices := []float64{100.0}

	// Act
	result := CalculateSlope(prices)

	// Assert
	assert.Equal(t, 0.0, result)
}

func TestCalculateSlope_Empty_ReturnsZero(t *testing.T) {
	// Arrange
	prices := []float64{}

	// Act
	result := CalculateSlope(prices)

	// Assert
	assert.Equal(t, 0.0, result)
}

func TestCalculateSlope_StartValueZero_NoNaNOrPanic(t *testing.T) {
	// Arrange — startVal=0 should fallback to 1e-9
	prices := []float64{0.0, 10.0, 20.0}

	// Act
	result := CalculateSlope(prices)

	// Assert
	assert.False(t, math.IsNaN(result))
	assert.False(t, math.IsInf(result, 0))
}
