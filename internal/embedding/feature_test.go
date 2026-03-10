package embedding

import (
	"testing"
	"time"
	"time-series-rag-agent/internal/exchange"

	"github.com/stretchr/testify/assert"
)

func makeHistory(closes []float64) []exchange.WsRestCandle {
	history := make([]exchange.WsRestCandle, len(closes))
	for i, c := range closes {
		history[i] = exchange.WsRestCandle{
			Time:  int64(1000000 + i*900),
			Close: c,
		}
	}
	return history
}

// --- NewFeatureCalculator ---

func TestNewFeatureCalculator_SetsFields(t *testing.T) {
	// Arrange & Act
	fc := NewFeatureCalculator("BTCUSDT", "1h", 5)

	// Assert
	assert.Equal(t, "BTCUSDT", fc.Symbol)
	assert.Equal(t, "1h", fc.Interval)
	assert.Equal(t, 5, fc.VectorWindow)
}

// --- Calculate: nil cases ---

func TestCalculate_TooShortHistory_ReturnsNil(t *testing.T) {
	// Arrange — need VectorWindow+1=6 candles, got 2
	fc := NewFeatureCalculator("BTCUSDT", "1h", 5)
	history := makeHistory([]float64{100.0, 110.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.Nil(t, result)
}

func TestCalculate_EmptyHistory_ReturnsNil(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 3)

	// Act
	result := fc.Calculate([]exchange.WsRestCandle{})

	// Assert
	assert.Nil(t, result)
}

// --- Calculate: metadata ---

func TestCalculate_Symbol_PropagatedToFeature(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("SOLUSDT", "4h", 2)
	history := makeHistory([]float64{50.0, 55.0, 60.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, "SOLUSDT", result.Symbol)
}

func TestCalculate_Interval_PropagatedToFeature(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("SOLUSDT", "4h", 2)
	history := makeHistory([]float64{50.0, 55.0, 60.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, "4h", result.Interval)
}

func TestCalculate_Time_EqualsLastCandleTime(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 2)
	history := []exchange.WsRestCandle{
		{Time: 1000, Close: 100.0},
		{Time: 2000, Close: 110.0},
		{Time: 3000, Close: 120.0},
	}

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, time.Unix(3000, 0), result.Time)
}

func TestCalculate_ClosePrice_EqualsLastCandle(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 3)
	history := makeHistory([]float64{100.0, 110.0, 105.0, 999.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, 999.0, result.ClosePrice)
}

// --- Calculate: embedding shape ---

func TestCalculate_EmbeddingLength_EqualsVectorWindow(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 4)
	history := makeHistory([]float64{100.0, 102.0, 104.0, 106.0, 108.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	assert.Len(t, result.Embedding, 4)
}

// --- Calculate: embedding values ---

func TestCalculate_Embedding_MatchesManualZScoreOfLogReturns(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 3)
	closes := []float64{100.0, 110.0, 121.0, 133.1}
	history := makeHistory(closes)

	// Manually compute expected embedding
	logReturns := CalculateLogReturn(closes)
	expectedEmbedding := CalculateZScore(logReturns)

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	assert.InDeltaSlice(t, expectedEmbedding, result.Embedding, 1e-9)
}

func TestCalculate_FlatPrices_EmbeddingNearZero(t *testing.T) {
	// Arrange — flat prices → log returns = 0 → z-score = 0
	fc := NewFeatureCalculator("BTCUSDT", "1h", 3)
	history := makeHistory([]float64{100.0, 100.0, 100.0, 100.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	for _, v := range result.Embedding {
		assert.InDelta(t, 0.0, v, 1e-6)
	}
}

func TestCalculate_IncreasingPrices_EmbeddingValuesFinite(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 4)
	history := makeHistory([]float64{100.0, 110.0, 121.0, 133.1, 146.41})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	for _, v := range result.Embedding {
		assert.False(t, isNaN(v), "embedding value should not be NaN")
		assert.False(t, isInf(v), "embedding value should not be Inf")
	}
}

func TestCalculate_Embedding_ZScoreMeanNearZero(t *testing.T) {
	// Arrange — z-score property: mean of result ≈ 0
	fc := NewFeatureCalculator("BTCUSDT", "1h", 4)
	history := makeHistory([]float64{100.0, 105.0, 98.0, 112.0, 107.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	sum := 0.0
	for _, v := range result.Embedding {
		sum += v
	}
	mean := sum / float64(len(result.Embedding))
	assert.InDelta(t, 0.0, mean, 1e-9)
}

// --- Calculate: window isolation ---

func TestCalculate_UsesOnlyLastWindow_IgnoresOlderCandles(t *testing.T) {
	// Arrange — window=2, only last 3 candles matter
	// historyA has junk data at the start that should be ignored
	fc := NewFeatureCalculator("BTCUSDT", "1h", 2)

	historyA := makeHistory([]float64{999.0, 1.0, 100.0, 110.0, 120.0})
	historyB := makeHistory([]float64{100.0, 110.0, 120.0})

	// Act
	resultA := fc.Calculate(historyA)
	resultB := fc.Calculate(historyB)

	// Assert — same window → same embedding
	assert.NotNil(t, resultA)
	assert.NotNil(t, resultB)
	assert.InDeltaSlice(t, resultB.Embedding, resultA.Embedding, 1e-9)
}

func TestCalculate_UsesOnlyLastWindow_ClosePriceIsLast(t *testing.T) {
	// Arrange
	fc := NewFeatureCalculator("BTCUSDT", "1h", 2)
	history := makeHistory([]float64{50.0, 60.0, 70.0, 80.0, 999.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.Equal(t, 999.0, result.ClosePrice)
}

func TestCalculate_HistoryExactlyVectorWindow_ReturnsNil(t *testing.T) {
	// Arrange — reqLen = VectorWindow+1, history = VectorWindow → ไม่พอ
	fc := NewFeatureCalculator("BTCUSDT", "1h", 4)
	history := makeHistory([]float64{100.0, 102.0, 104.0, 106.0}) // len=4 == vectorWindow

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.Nil(t, result)
}

func TestCalculate_ZeroClosePrice_EmbeddingFinite(t *testing.T) {
	// Arrange — PlanckConstant prevents log(0), should not panic or NaN
	fc := NewFeatureCalculator("BTCUSDT", "1h", 3)
	history := makeHistory([]float64{0.0, 100.0, 110.0, 120.0})

	// Act
	result := fc.Calculate(history)

	// Assert
	assert.NotNil(t, result)
	for _, v := range result.Embedding {
		assert.False(t, isNaN(v), "should not be NaN on zero close")
		assert.False(t, isInf(v), "should not be Inf on zero close")
	}
}

func TestCalculate_LargerHistory_OnlyLastWindowAffectsEmbedding(t *testing.T) {
	// Arrange — ยืนยันว่า candle ก่อน window ไม่กระทบ embedding เลย
	fc := NewFeatureCalculator("BTCUSDT", "1h", 3)

	base := makeHistory([]float64{100.0, 110.0, 121.0, 133.1})
	withPrefix := makeHistory([]float64{1.0, 2.0, 3.0, 100.0, 110.0, 121.0, 133.1})

	// Act
	resultBase := fc.Calculate(base)
	resultWithPrefix := fc.Calculate(withPrefix)

	// Assert
	assert.InDeltaSlice(t, resultBase.Embedding, resultWithPrefix.Embedding, 1e-9)
	assert.Equal(t, resultBase.ClosePrice, resultWithPrefix.ClosePrice)
}

// -- test calculator

// --- helpers ---

func isNaN(v float64) bool {
	return v != v
}

func isInf(v float64) bool {
	return v > 1e308 || v < -1e308
}
