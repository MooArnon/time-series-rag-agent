//go:build integration

package exchange

import (
	"context"
	"os"
	"testing"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/stretchr/testify/assert"
)

// --- Tests ---

// Test that FetchLatestCandles successfully retrieves the n latest candles from Binance.
func TestIntegrationFetchLatestCandles_Success(t *testing.T) {

	binanceClient := futures.NewClient(os.Getenv("BINANCE_API_KEY"), os.Getenv("BINANCE_SECRET"))
	adapter := NewBinanceAdapter(binanceClient)

	// Act
	candles, err := FetchLatestCandles(context.Background(), adapter, "ETHUSDT", "15m", 30)

	// Basic checks
	assert.NoError(t, err)
	assert.Len(t, candles, 30)

	// Sanity check
	c := candles[0]
	assert.NotZero(t, c.Time)
	assert.NotZero(t, c.Close)
	assert.GreaterOrEqual(t, c.High, c.Low)
}
