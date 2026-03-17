package exchange

import (
	"context"
	"fmt"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/stretchr/testify/assert"
)

// --- Mock ---
type mockKlineService struct {
	returnData []*futures.Kline
	returnErr  error
}

func (m *mockKlineService) FetchKlines(ctx context.Context, symbol, interval string, limit int) ([]*futures.Kline, error) {
	return m.returnData, m.returnErr
}

// --- Tests ---
func TestFetchLatestCandles_Success(t *testing.T) {
	// Arrange
	mock := &mockKlineService{
		returnData: []*futures.Kline{
			{OpenTime: 1000000, Open: "100.0", High: "105.0", Low: "99.0", Close: "103.0", Volume: "500.0"},
			{OpenTime: 1000900, Open: "103.0", High: "108.0", Low: "102.0", Close: "107.0", Volume: "600.0"},
			{OpenTime: 1001800, Open: "107.0", High: "110.0", Low: "106.0", Close: "109.0", Volume: "700.0"}, // ← incomplete candle
		},
	}

	// Act
	candles, err := FetchLatestCandles(context.Background(), mock, "ETHUSDT", "15m", 3)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, candles, 2) // drop last → 2
	assert.Equal(t, 103.0, candles[0].Close)
	assert.Equal(t, 107.0, candles[1].Close)
}

func TestFetchLatestCandles_APIError(t *testing.T) {

	// Arrange
	mock := &mockKlineService{
		returnErr: fmt.Errorf("binance timeout"),
	}

	// Act
	candles, err := FetchLatestCandles(context.Background(), mock, "ETHUSDT", "15m", 2)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, candles)
	assert.ErrorContains(t, err, "binance timeout")
}

func TestFetchLatestCandles_ParseError(t *testing.T) {

	// Arrange — ส่ง string ที่ parse เป็น float ไม่ได้
	mock := &mockKlineService{
		returnData: []*futures.Kline{
			{OpenTime: 1000000, Open: "NOT_A_NUMBER", High: "105.0", Low: "99.0", Close: "103.0", Volume: "500.0"},
		},
	}

	// Act
	candles, err := FetchLatestCandles(context.Background(), mock, "ETHUSDT", "15m", 1)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, candles)
}
