package trade

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Tests ---

func TestCalculateStopLossPriceZeroLeverage_Error(t *testing.T) {

	_, err := CalculateStopLossPrice(0, 5, "HOLD", 100)
	assert.Equal(t, err, fmt.Errorf("leverage must be greater than zero, got %d", 0))

}

// Test that FetchLatestCandles successfully retrieves the n latest candles from Binance.
func TestCalculateStopLossPriceNuetralLeverageLONG_Success(t *testing.T) {

	tp, _ := CalculateStopLossPrice(1, 5, "LONG", 100)
	assert.Equal(t, tp, float64(95))

}

func TestCalculateStopLossPriceLeverageLONG1_Success(t *testing.T) {

	tp, _  := CalculateStopLossPrice(2, 5, "LONG", 100)
	assert.Greater(t, tp, float64(88.4990))
	assert.LessOrEqual(t, tp, float64(97.5))

}


func TestCalculateStopLossPriceLeverageLONG2_Success(t *testing.T) {

	tp, _  := CalculateStopLossPrice(2, 5, "LONG", 34.3)
	assert.Greater(t, tp, float64(33.4424999))
	assert.LessOrEqual(t, tp, float64(33.4425))

}

func TestCalculateStopLossPriceNuetralLeverageSHORT_Success(t *testing.T) {

	tp, _ := CalculateStopLossPrice(1, 5, "SHORT", 100)
	assert.Equal(t, tp, float64(105))

}

func TestCalculateStopLossPriceNuetralLeverageSHORT1_Success(t *testing.T) {

	tp, _ := CalculateStopLossPrice(7, 5, "SHORT", 78.92)
	assert.Greater(t, tp, float64(79.4837142856))
	assert.LessOrEqual(t, tp, float64(79.4837142858))

}

func TestCalculateStopLossPriceNuetralLeverageSHORT_Error(t *testing.T) {

	_, err := CalculateStopLossPrice(1, 5, "HOLD", 100)
	assert.Equal(t, err, fmt.Errorf("position direction must be LONG or SHORT, got %q", "HOLD"))

}
