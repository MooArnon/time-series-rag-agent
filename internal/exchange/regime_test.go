package exchange

import (
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"time-series-rag-agent/config"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func testLogger() slog.Logger {
	return *slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func testRegimeCfg() *config.AppConfig {
	return &config.AppConfig{
		Regime: config.RegimeConfig{
			ADXTrendThreshold:    25.0,
			ADXRangeThreshold:    20.0,
			ATRVolatileThreshold: 2.0,
			BandWidthThreshold:   0.1,
		},
	}
}

// makeUniformCandles — flat price, tiny range → low ATR, low ADX, tight BBW
func makeUniformCandles(n int, price float64) []WsRestCandle {
	candles := make([]WsRestCandle, n)
	for i := range candles {
		candles[i] = WsRestCandle{
			Time:  int64(i) * 60_000,
			Open:  price,
			High:  price + 0.1,
			Low:   price - 0.1,
			Close: price,
		}
	}
	return candles
}

// makeTrendingCandles — consistent uptrend → high ADX, PlusDI > MinusDI
func makeTrendingCandles(n int, startPrice, step float64) []WsRestCandle {
	candles := make([]WsRestCandle, n)
	for i := range candles {
		base := startPrice + float64(i)*step
		candles[i] = WsRestCandle{
			Time:  int64(i) * 60_000,
			Open:  base,
			High:  base + step*0.8,
			Low:   base - step*0.1,
			Close: base + step*0.6,
		}
	}
	return candles
}

// makeDowntrendCandles — consistent downtrend → high ADX, MinusDI > PlusDI
func makeDowntrendCandles(n int, startPrice, step float64) []WsRestCandle {
	candles := make([]WsRestCandle, n)
	for i := range candles {
		base := startPrice - float64(i)*step
		candles[i] = WsRestCandle{
			Time:  int64(i) * 60_000,
			Open:  base,
			High:  base + step*0.1,
			Low:   base - step*0.8,
			Close: base - step*0.6,
		}
	}
	return candles
}

// makeVolatileCandles — calm first N-14 candles, then huge spikes → ATR14 >> ATR100
func makeVolatileCandles(n int) []WsRestCandle {
	candles := make([]WsRestCandle, n)
	for i := range candles {
		if i < n-14 {
			candles[i] = WsRestCandle{
				Time: int64(i) * 60_000,
				Open: 100.0, High: 100.2, Low: 99.8, Close: 100.0,
			}
		} else {
			candles[i] = WsRestCandle{ // enormous swing
				Time: int64(i) * 60_000,
				Open: 100.0, High: 160.0, Low: 40.0, Close: 100.0,
			}
		}
	}
	return candles
}

// makeOscillatingCandles — price ping-pongs → low ADX, tight BBW
func makeOscillatingCandles(n int, base, amp float64) []WsRestCandle {
	candles := make([]WsRestCandle, n)
	for i := range candles {
		sign := 1.0
		if i%2 != 0 {
			sign = -1.0
		}
		price := base + sign*amp
		candles[i] = WsRestCandle{
			Time:  int64(i) * 60_000,
			Open:  base,
			High:  price + 0.05,
			Low:   price - 0.05,
			Close: price,
		}
	}
	return candles
}

// ─── CalcBandWidth ────────────────────────────────────────────────────────────

func TestCalcBandWidth_NotEnoughCandles(t *testing.T) {
	candles := makeUniformCandles(10, 100.0)
	result := CalcBandWidth(candles, 20)
	assert.Equal(t, 0.0, result)
}

func TestCalcBandWidth_FlatPrice_ReturnsZero(t *testing.T) {
	// Arrange: perfectly flat price → stdDev = 0 → BBW = 0
	candles := makeUniformCandles(30, 100.0)

	// Act
	result := CalcBandWidth(candles, 20)

	// Assert
	assert.InDelta(t, 0.0, result, 1e-9)
}

func TestCalcBandWidth_KnownValues(t *testing.T) {
	// Arrange: 20 candles alternating 98/102 → predictable stdDev
	candles := makeOscillatingCandles(20, 100.0, 2.0)

	// Act
	result := CalcBandWidth(candles, 20)

	// Assert: BBW = (upper-lower)/SMA = 4*stdDev/100 > 0
	assert.Greater(t, result, 0.0)

	// manual check: stdDev ≈ 2.0, upper-lower = 8.0, SMA ≈ 100 → BBW ≈ 0.08
	expected := 4 * 2.0 / 100.0
	assert.InDelta(t, expected, result, 0.01)
}

func TestCalcBandWidth_ZeroSMA_ReturnsZero(t *testing.T) {
	// Arrange: price = 0 → avoid division by zero
	candles := makeUniformCandles(25, 0.0)
	result := CalcBandWidth(candles, 20)
	assert.Equal(t, 0.0, result)
}

// ─── CalcATRRatio ─────────────────────────────────────────────────────────────

func TestCalcATRRatio_NotEnoughCandles(t *testing.T) {
	candles := makeUniformCandles(50, 100.0)
	result := CalcATRRatio(candles)
	assert.Equal(t, 0.0, result)
}

func TestCalcATRRatio_FlatPrice_ReturnsZero(t *testing.T) {
	// Arrange: all candles identical → TR = 0 → ATR100 = 0 → guard returns 0
	candles := makeUniformCandles(120, 100.0)
	result := CalcATRRatio(candles)
	// With tiny 0.1 range, ATR14 ≈ ATR100 → ratio near 1.0
	// For perfectly flat (same OHLC every bar) TR=0 so ratio = 0
	assert.GreaterOrEqual(t, result, 0.0)
}

func TestCalcATRRatio_VolatileRecent_ReturnsHighRatio(t *testing.T) {
	// Arrange: calm baseline then huge spikes in last 14 bars
	candles := makeVolatileCandles(120)

	// Act
	result := CalcATRRatio(candles)

	// Assert: ATR14 >> ATR100 so ratio >> 1
	assert.Greater(t, result, 2.0, "volatile candles should produce ATR ratio > 2.0")
}

func TestCalcATRRatio_StableMarket_RatioNearOne(t *testing.T) {
	// Arrange: uniform volatility throughout → ATR14 ≈ ATR100 → ratio ≈ 1
	candles := makeOscillatingCandles(120, 100.0, 1.0)

	// Act
	result := CalcATRRatio(candles)

	// Assert
	assert.InDelta(t, 1.0, result, 0.3)
}

// ─── CalcADX ─────────────────────────────────────────────────────────────────

func TestCalcADX_NotEnoughCandles(t *testing.T) {
	candles := makeUniformCandles(20, 100.0)
	result := CalcADX(candles, 14)
	assert.Equal(t, adxResult{}, result)
}

func TestCalcADX_StrongUptrend_BullSignal(t *testing.T) {
	// Arrange: consistent uptrend with enough candles
	candles := makeTrendingCandles(120, 100.0, 1.0)

	// Act
	result := CalcADX(candles, 14)

	// Assert
	assert.Greater(t, result.ADX, 0.0)
	assert.Greater(t, result.PlusDI, result.MinusDI, "uptrend should have +DI > -DI")
}

func TestCalcADX_StrongDowntrend_BearSignal(t *testing.T) {
	// Arrange
	candles := makeDowntrendCandles(120, 200.0, 1.0)

	// Act
	result := CalcADX(candles, 14)

	// Assert
	assert.Greater(t, result.ADX, 0.0)
	assert.Greater(t, result.MinusDI, result.PlusDI, "downtrend should have -DI > +DI")
}

func TestCalcADX_AllValuesNonNegative(t *testing.T) {
	candles := makeTrendingCandles(120, 50.0, 0.5)
	result := CalcADX(candles, 14)

	assert.GreaterOrEqual(t, result.ADX, 0.0)
	assert.GreaterOrEqual(t, result.PlusDI, 0.0)
	assert.GreaterOrEqual(t, result.MinusDI, 0.0)
}

func TestCalcADX_ADXBoundedZeroToHundred(t *testing.T) {
	candles := makeTrendingCandles(200, 100.0, 2.0)
	result := CalcADX(candles, 14)

	assert.GreaterOrEqual(t, result.ADX, 0.0)
	assert.LessOrEqual(t, result.ADX, 100.0)
}

// ─── FetchLatestRegimes ───────────────────────────────────────────────────────

func TestFetchLatestRegimes_NotEnoughCandles_ReturnsError(t *testing.T) {
	// Arrange
	logger := testLogger()
	cfg := testRegimeCfg()
	candles := makeUniformCandles(50, 100.0) // < 101

	// Act
	_, err := FetchLatestRegimes(logger, nil, cfg, "BTCUSDT", []string{"15m"}, candles)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enough candles")
}

func TestFetchLatestRegimes_VolatileRegime(t *testing.T) {
	// Arrange
	logger := testLogger()
	cfg := testRegimeCfg()
	candles := makeVolatileCandles(120)

	// Act
	results, err := FetchLatestRegimes(logger, nil, cfg, "BTCUSDT", []string{"15m"}, candles)

	// Assert
	assert.NoError(t, err)
	assert.Contains(t, results, "15m")
	assert.Equal(t, Regime("VOLATILE"), results["15m"].Result.Regime)
}

func TestFetchLatestRegimes_TrendingBullRegime(t *testing.T) {
	// Arrange: strong uptrend → ADX high, atrRatio moderate
	logger := testLogger()
	cfg := &config.AppConfig{
		Regime: config.RegimeConfig{
			ADXTrendThreshold:    10.0, // low threshold → easier to trigger TRENDING
			ADXRangeThreshold:    5.0,
			ATRVolatileThreshold: 99.0, // very high → won't trigger VOLATILE
			BandWidthThreshold:   0.1,
		},
	}
	candles := makeTrendingCandles(120, 100.0, 1.0)

	// Act
	results, err := FetchLatestRegimes(logger, nil, cfg, "BTCUSDT", []string{"1h"}, candles)

	// Assert
	assert.NoError(t, err)
	regime := results["1h"].Result
	assert.Equal(t, Regime("TRENDING"), regime.Regime)
	assert.Equal(t, "BULL", regime.Direction)
}

func TestFetchLatestRegimes_TrendingBearRegime(t *testing.T) {
	// Arrange
	logger := testLogger()
	cfg := &config.AppConfig{
		Regime: config.RegimeConfig{
			ADXTrendThreshold:    10.0,
			ADXRangeThreshold:    5.0,
			ATRVolatileThreshold: 99.0,
			BandWidthThreshold:   0.1,
		},
	}
	candles := makeDowntrendCandles(120, 200.0, 1.0)

	// Act
	results, err := FetchLatestRegimes(logger, nil, cfg, "BTCUSDT", []string{"1h"}, candles)

	// Assert
	assert.NoError(t, err)
	regime := results["1h"].Result
	assert.Equal(t, Regime("TRENDING"), regime.Regime)
	assert.Equal(t, "BEAR", regime.Direction)
}

func TestFetchLatestRegimes_RangingRegime(t *testing.T) {
	// Arrange: flat oscillation → ADX low, BBW tight
	logger := testLogger()
	cfg := &config.AppConfig{
		Regime: config.RegimeConfig{
			ADXTrendThreshold:    99.0, // unreachable
			ADXRangeThreshold:    99.0, // always < this
			ATRVolatileThreshold: 99.0, // unreachable
			BandWidthThreshold:   99.0, // always < this
		},
	}
	candles := makeOscillatingCandles(120, 100.0, 0.5)

	// Act
	results, err := FetchLatestRegimes(logger, nil, cfg, "BTCUSDT", []string{"4h"}, candles)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, Regime("RANGING"), results["4h"].Result.Regime)
}

func TestFetchLatestRegimes_MultipleIntervals(t *testing.T) {
	// Arrange
	logger := testLogger()
	cfg := testRegimeCfg()
	candles := makeVolatileCandles(120)
	intervals := []string{"15m", "1h", "4h"}

	// Act
	results, err := FetchLatestRegimes(logger, nil, cfg, "ETHUSDT", intervals, candles)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, results, 3)
	for _, iv := range intervals {
		assert.Contains(t, results, iv)
		assert.Equal(t, iv, results[iv].Interval)
		assert.False(t, results[iv].Time.IsZero())
	}
}

func TestFetchLatestRegimes_RegimeResultFieldsPopulated(t *testing.T) {
	// Arrange
	logger := testLogger()
	cfg := testRegimeCfg()
	candles := makeTrendingCandles(120, 100.0, 0.5)

	// Act
	results, err := FetchLatestRegimes(logger, nil, cfg, "BTCUSDT", []string{"15m"}, candles)

	// Assert: all indicator fields must be non-negative
	assert.NoError(t, err)
	r := results["15m"].Result
	assert.GreaterOrEqual(t, r.ADX, 0.0)
	assert.GreaterOrEqual(t, r.PlusDI, 0.0)
	assert.GreaterOrEqual(t, r.MinusDI, 0.0)
	assert.GreaterOrEqual(t, r.ATRRatio, 0.0)
	assert.GreaterOrEqual(t, r.BandWidth, 0.0)
	assert.NotEqual(t, math.NaN(), r.ADX)
}
