package embedding

import (
	"testing"

	"time-series-rag-agent/internal/exchange"

	"github.com/stretchr/testify/assert"
)

func TestMergeCandles_RestOnly(t *testing.T) {
	// Arrange
	rest := []exchange.RestCandle{
		{Time: 1000, Open: 100.0, High: 105.0, Low: 99.0, Close: 103.0, Volume: 500.0},
		{Time: 2000, Open: 103.0, High: 108.0, Low: 102.0, Close: 107.0, Volume: 600.0},
	}
	ws := []exchange.WsCandle{}

	// Act
	result := MergeCandles(ws, rest)

	// Assert
	assert.Len(t, result, 2)
	assert.Equal(t, int64(1000), result[0].Time)
	assert.Equal(t, int64(2000), result[1].Time)
}

func TestMergeCandles_WsOnly(t *testing.T) {
	// Arrange
	rest := []exchange.RestCandle{}
	ws := []exchange.WsCandle{
		{Time: 1000, Open: 100.0, High: 105.0, Low: 99.0, Close: 103.0, Volume: 500.0},
		{Time: 2000, Open: 103.0, High: 108.0, Low: 102.0, Close: 107.0, Volume: 600.0},
	}

	// Act
	result := MergeCandles(ws, rest)

	// Assert
	assert.Len(t, result, 2)
	assert.Equal(t, int64(1000), result[0].Time)
	assert.Equal(t, int64(2000), result[1].Time)
}

func TestMergeCandles_WsOverwritesRestOnSameTime(t *testing.T) {
	// Arrange
	rest := []exchange.RestCandle{
		{Time: 1000, Open: 100.0, High: 105.0, Low: 99.0, Close: 103.0, Volume: 500.0},
	}
	ws := []exchange.WsCandle{
		{Time: 1000, Open: 200.0, High: 210.0, Low: 195.0, Close: 205.0, Volume: 999.0},
	}

	// Act
	result := MergeCandles(ws, rest)

	// Assert
	assert.Len(t, result, 1)
	assert.Equal(t, 205.0, result[0].Close)
	assert.Equal(t, 999.0, result[0].Volume)
}

func TestMergeCandles_MergeAndSortAsc(t *testing.T) {
	// Arrange
	rest := []exchange.RestCandle{
		{Time: 3000, Open: 110.0, High: 115.0, Low: 109.0, Close: 112.0, Volume: 700.0},
		{Time: 1000, Open: 100.0, High: 105.0, Low: 99.0, Close: 103.0, Volume: 500.0},
	}
	ws := []exchange.WsCandle{
		{Time: 2000, Open: 103.0, High: 108.0, Low: 102.0, Close: 107.0, Volume: 600.0},
	}

	// Act
	result := MergeCandles(ws, rest)

	// Assert
	assert.Len(t, result, 3)
	assert.Equal(t, int64(1000), result[0].Time)
	assert.Equal(t, int64(2000), result[1].Time)
	assert.Equal(t, int64(3000), result[2].Time)
}

func TestMergeCandles_BothEmpty(t *testing.T) {
	// Arrange
	rest := []exchange.RestCandle{}
	ws := []exchange.WsCandle{}

	// Act
	result := MergeCandles(ws, rest)

	// Assert
	assert.Len(t, result, 0)
}
