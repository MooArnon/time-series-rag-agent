package embedding

import (
	"testing"
	"time-series-rag-agent/internal/exchange"

	"github.com/stretchr/testify/assert"
)

// makeHistoryWithTime creates exchange.WsRestCandle with explicit times and closes
func makeHistoryWithTime(entries [][2]float64) []exchange.WsRestCandle {
	history := make([]exchange.WsRestCandle, len(entries))
	for i, e := range entries {
		history[i] = exchange.WsRestCandle{Time: int64(e[0]), Close: e[1]}
	}
	return history
}

// --- CalculateFromHistory ---

func TestCalculateFromHistory_EmptyHistory_ReturnsEmpty(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()

	// Act
	result := lc.CalculateFromHistory([]exchange.WsRestCandle{})

	// Assert
	assert.Empty(t, result)
}

func TestCalculateFromHistory_SingleCandle_ReturnsEmpty(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{100.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	assert.Empty(t, result)
}

func TestCalculateFromHistory_NextReturn_CorrectValue(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// T-1=100, T=120 → return = (120-100)/100 = 0.2
	history := makeHistory([]float64{100.0, 120.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	assert.Len(t, result, 1)
	assert.Equal(t, "next_return", result[0].Column)
	assert.InDelta(t, 0.2, result[0].Value, 1e-9)
}

func TestCalculateFromHistory_NextReturn_NegativeReturn(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// T-1=200, T=150 → return = (150-200)/200 = -0.25
	history := makeHistory([]float64{200.0, 150.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	assert.InDelta(t, -0.25, result[0].Value, 1e-9)
}

func TestCalculateFromHistory_NextReturn_TargetTimeIsTMinus1(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistoryWithTime([][2]float64{
		{1000, 100.0},
		{2000, 110.0},
	})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	assert.Equal(t, int64(1000), result[0].TargetTime)
}

func TestCalculateFromHistory_NextReturn_PrevCloseZero_Skipped(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{0.0, 110.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	assert.Empty(t, result)
}

func TestCalculateFromHistory_Slope3_CorrectValue(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// T-3=100, future 3 candles = [110, 120, 130] → steadily increasing → positive slope
	// slope is computed on normalized prices relative to first future candle (110)
	// yNorm: [0, 0.0909, 0.1818] with x=[0,1,2] → known positive linear slope
	history := makeHistory([]float64{100.0, 110.0, 120.0, 130.0})
	expectedSlope := CalculateSlope([]float64{110.0, 120.0, 130.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	slope3 := findByColumn(result, "next_slope_3")
	assert.NotNil(t, slope3)
	assert.InDelta(t, expectedSlope, slope3.Value, 1e-9)
}

func TestCalculateFromHistory_Slope3_TargetTimeIsTMinus3(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistoryWithTime([][2]float64{
		{1000, 100.0},
		{2000, 110.0},
		{3000, 120.0},
		{4000, 130.0},
	})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	slope3 := findByColumn(result, "next_slope_3")
	assert.NotNil(t, slope3)
	assert.Equal(t, int64(1000), slope3.TargetTime)
}

func TestCalculateFromHistory_Slope5_CorrectValue(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// T-5=100, future 5 candles = [102, 104, 106, 108, 110]
	history := makeHistory([]float64{100.0, 102.0, 104.0, 106.0, 108.0, 110.0})
	expectedSlope := CalculateSlope([]float64{102.0, 104.0, 106.0, 108.0, 110.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	slope5 := findByColumn(result, "next_slope_5")
	assert.NotNil(t, slope5)
	assert.InDelta(t, expectedSlope, slope5.Value, 1e-9)
}

func TestCalculateFromHistory_Slope5_TargetTimeIsTMinus5(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistoryWithTime([][2]float64{
		{1000, 100.0},
		{2000, 102.0},
		{3000, 104.0},
		{4000, 106.0},
		{5000, 108.0},
		{6000, 110.0},
	})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	slope5 := findByColumn(result, "next_slope_5")
	assert.NotNil(t, slope5)
	assert.Equal(t, int64(1000), slope5.TargetTime)
}

func TestCalculateFromHistory_DecreasingPrices_NegativeSlopes(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{130.0, 120.0, 110.0, 100.0, 90.0, 80.0})

	// Act
	result := lc.CalculateFromHistory(history)

	// Assert
	slope3 := findByColumn(result, "next_slope_3")
	slope5 := findByColumn(result, "next_slope_5")
	assert.Less(t, slope3.Value, 0.0)
	assert.Less(t, slope5.Value, 0.0)
}

// --- CalculateLookahead ---

func TestCalculateLookahead_NoFutureData_ReturnsEmpty(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{100.0, 110.0})

	// Act — idx=1 is the last element, nothing ahead
	result := lc.CalculateLookahead(history, 1, 9999)

	// Assert
	assert.Empty(t, result)
}

func TestCalculateLookahead_NextReturn_CorrectValue(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// idx=1 close=110, idx+1 close=132 → return = (132-110)/110 = 0.2
	history := makeHistory([]float64{100.0, 110.0, 132.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	nr := findByColumn(result, "next_return")
	assert.NotNil(t, nr)
	assert.InDelta(t, 0.2, nr.Value, 1e-9)
}

func TestCalculateLookahead_NextReturn_NegativeReturn(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// idx=1 close=200, idx+1 close=150 → return = (150-200)/200 = -0.25
	history := makeHistory([]float64{100.0, 200.0, 150.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	nr := findByColumn(result, "next_return")
	assert.NotNil(t, nr)
	assert.InDelta(t, -0.25, nr.Value, 1e-9)
}

func TestCalculateLookahead_NextReturn_UsesTargetTimeParam(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{100.0, 110.0, 121.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	assert.Equal(t, int64(5000), result[0].TargetTime)
}

func TestCalculateLookahead_Slope3_CorrectValue(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// idx=1, future 3 = [120, 130, 140]
	history := makeHistory([]float64{100.0, 110.0, 120.0, 130.0, 140.0})
	expectedSlope := CalculateSlope([]float64{120.0, 130.0, 140.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	slope3 := findByColumn(result, "next_slope_3")
	assert.NotNil(t, slope3)
	assert.InDelta(t, expectedSlope, slope3.Value, 1e-9)
}

func TestCalculateLookahead_Slope5_CorrectValue(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// idx=1, future 5 = [104, 106, 108, 110, 112]
	history := makeHistory([]float64{100.0, 102.0, 104.0, 106.0, 108.0, 110.0, 112.0})
	expectedSlope := CalculateSlope([]float64{104.0, 106.0, 108.0, 110.0, 112.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	slope5 := findByColumn(result, "next_slope_5")
	assert.NotNil(t, slope5)
	assert.InDelta(t, expectedSlope, slope5.Value, 1e-9)
}

func TestCalculateLookahead_Slope3NotAvailable_WhenOnlyTwoFuture(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	// idx=1, only 2 future candles → slope3 needs 3 ahead (idx+3 < n fails)
	history := makeHistory([]float64{100.0, 110.0, 120.0, 130.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	columns := columnsOf(result)
	assert.NotContains(t, columns, "next_slope_3")
}

func TestCalculateLookahead_AllLabels_UseTargetTime(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{100.0, 102.0, 104.0, 106.0, 108.0, 110.0, 112.0})

	// Act
	result := lc.CalculateLookahead(history, 1, 7777)

	// Assert
	for _, u := range result {
		assert.Equal(t, int64(7777), u.TargetTime, "column %s should have targetTime 7777", u.Column)
	}
}

func TestCalculateLookahead_PrevCloseZero_NextReturnSkipped(t *testing.T) {
	// Arrange
	lc := NewLabelCalculator()
	history := makeHistory([]float64{100.0, 0.0, 110.0})

	// Act — idx=1 has Close=0
	result := lc.CalculateLookahead(history, 1, 5000)

	// Assert
	assert.NotContains(t, columnsOf(result), "next_return")
}

// --- helpers ---

func columnsOf(updates []LabelUpdate) []string {
	cols := make([]string, len(updates))
	for i, u := range updates {
		cols[i] = u.Column
	}
	return cols
}

func findByColumn(updates []LabelUpdate, column string) *LabelUpdate {
	for i := range updates {
		if updates[i].Column == column {
			return &updates[i]
		}
	}
	return nil
}
