package trade

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Tests ---

func TestCalculateTakeProfitPriceZeroLeverage_Error(t *testing.T) {

	_, err := CalculateTakeProfitPrice(0, 5, "HOLD", 100)
	assert.Equal(t, err, fmt.Errorf("leverage must be greater than zero, got %d", 0))

}

// Test that FetchLatestCandles successfully retrieves the n latest candles from Binance.
func TestCalculateTakeProfitPriceNuetralLeverageLONG_Success(t *testing.T) {

	tp, _ := CalculateTakeProfitPrice(1, 5, "LONG", 100)
	assert.Equal(t, tp, float64(105))

}

func TestCalculateTakeProfitPriceLeverageLONG1_Success(t *testing.T) {

	tp, _  := CalculateTakeProfitPrice(2, 5, "LONG", 100)
	assert.Greater(t, tp, float64(102.4990))
	assert.LessOrEqual(t, tp, float64(102.5))

}

func TestCalculateTakeProfitPriceLeverageLONG2_Success(t *testing.T) {

	tp, _  := CalculateTakeProfitPrice(2, 5, "LONG", 37.5)
	assert.Greater(t, tp, float64(38.437499))
	assert.LessOrEqual(t, tp, float64(38.4375))

}

func TestCalculateTakeProfitPriceLeverageLONG3_Success(t *testing.T) {

	tp, _  := CalculateTakeProfitPrice(5, 5, "LONG", 37.5)
	assert.Greater(t, tp, float64(37.87499))
	assert.LessOrEqual(t, tp, float64(37.875))

}

func TestCalculateTakeProfitPriceNuetralLeverageSHORT_Success(t *testing.T) {

	tp, _ := CalculateTakeProfitPrice(1, 5, "SHORT", 100)
	assert.Equal(t, tp, float64(95))

}

func TestCalculateTakeProfitPriceNuetralLeverageSHORT_Error(t *testing.T) {

	_, err := CalculateTakeProfitPrice(1, 5, "HOLD", 100)
	assert.Equal(t, err, fmt.Errorf("position direction must be LONG or SHORT, got %q", "HOLD"))

}
