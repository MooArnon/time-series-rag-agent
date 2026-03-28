package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/adshao/go-binance/v2/futures"

	"time-series-rag-agent/config"
)

type RegimeTrend struct {
}

type InputData struct {
	Time  int64
	Open  float64
	High  float64
	Low   float64
	Close float64
}

type Regime string

type RegimeConfig struct {
	ADXTrendThreshold    float64
	ADXRangeThreshold    float64
	ATRVolatileThreshold float64
	BandWidthThreshold   float64
	BandWidthPeriod      int
}

type IntervalRegime struct {
	Interval string
	Time     time.Time
	Result   RegimeResult
}

type RegimeResult struct {
	Regime    Regime
	Direction string // "BULL", "BEAR", ""
	ADX       float64
	PlusDI    float64
	MinusDI   float64
	ATRRatio  float64
	BandWidth float64
}

func FetchLatestRegimes(
	logger slog.Logger,
	client *futures.Client,
	cfg *config.AppConfig,
	symbol string,
	intervals []string,
) (map[string]IntervalRegime, error) {
	results := make(map[string]IntervalRegime)

	// candles ที่ต้องการต่อ interval (101 minimum + buffer)
	const fetchLimit = 120

	for _, interval := range intervals {
		klines, err := client.NewKlinesService().
			Symbol(symbol).
			Interval(interval).
			Limit(fetchLimit).
			Do(context.Background())
		if err != nil {
			return nil, fmt.Errorf("fetching %s candles: %w", interval, err)
		}

		candles, err := parseKLinesToRestCandle(klines)
		if err != nil {
			return nil, fmt.Errorf("fetching %s candles: %w", interval, err)
		}

		if len(candles) < 101 {
			logger.Error(fmt.Sprintf("Not enough candles for %s %s: got %d",
				symbol, interval, len(candles)))
			continue
		}

		adx := CalcADX(candles, 14)
		bbw := CalcBandWidth(candles, 20)
		atrRatio := CalcATRRatio(candles)

		result := RegimeResult{
			ADX:       adx.ADX,
			PlusDI:    adx.PlusDI,
			MinusDI:   adx.MinusDI,
			ATRRatio:  atrRatio,
			BandWidth: bbw,
			Regime:    "UNKNOWN",
		}

		switch {
		case atrRatio > cfg.Regime.ATRVolatileThreshold:
			result.Regime = "VOLATILE"

		case adx.ADX > cfg.Regime.ADXTrendThreshold && atrRatio < cfg.Regime.ATRVolatileThreshold:
			result.Regime = "TRENDING"
			if adx.PlusDI > adx.MinusDI {
				result.Direction = "BULL"
			} else {
				result.Direction = "BEAR"
			}

		case adx.ADX < cfg.Regime.ADXRangeThreshold && bbw < cfg.Regime.BandWidthThreshold:
			result.Regime = "RANGING"
		}

		// time ของ candle ล่าสุด
		latestCandle := candles[len(candles)-1]
		candleTime := time.Unix(latestCandle.Time, 0).UTC()

		results[interval] = IntervalRegime{
			Interval: interval,
			Time:     candleTime,
			Result:   result,
		}

		// logger.Info(fmt.Sprintf("Regime %s %s = %s (ADX=%.1f, ATR=%.3f)",
		// 	symbol, interval, result.ToRegimeLabel(), result.ADX, result.ATRRatio,
		// ))
	}

	return results, nil
}

func (r *RegimeTrend) PredictTrend(logger slog.Logger, Symbol string, Interval string, VectorWindow int, candle []RestCandle) (RegimeResult, error) {
	cfg := config.LoadConfig()
	logger.Info("Predicting market trend using regime detection...")

	// calculate ADX
	adx := CalcADX(candle, 14)
	logger.Info("Calculated ADX", "adx", adx.ADX)

	// calculate BBW
	bandWidth := CalcBandWidth(candle, 20)
	logger.Info("Calculated BBW", " bbw", bandWidth)

	// calculate ATR
	atrRatio := CalcATRRatio(candle)
	logger.Info("Calculated ATR Ratio", "atrRatio", atrRatio)

	result := RegimeResult{
		ADX:       adx.ADX,
		ATRRatio:  atrRatio,
		BandWidth: bandWidth,
		Regime:    "UNKNOWN",
	}

	switch {
	case atrRatio > cfg.Regime.ATRVolatileThreshold:
		result.Regime = "VOLATILE"

	case adx.ADX > cfg.Regime.ADXTrendThreshold && atrRatio < cfg.Regime.ATRVolatileThreshold:
		result.Regime = "TRENDING"
		if adx.PlusDI > adx.MinusDI {
			result.Direction = "BULL"
		} else {
			result.Direction = "BEAR"
		}

	case adx.ADX < cfg.Regime.ADXRangeThreshold && bandWidth < cfg.Regime.BandWidthThreshold:
		result.Regime = "RANGING"
	}

	return result, nil
}

// Calculations for regime detection would go here, but for simplicity, we return a fixed value. //
// ATR Ratio Calculation
func trueRange(c, prev RestCandle) float64 {
	return math.Max(
		c.High-c.Low,
		math.Max(
			math.Abs(c.High-prev.Close),
			math.Abs(c.Low-prev.Close),
		),
	)
}

func calcATR(candles []RestCandle, period int) float64 {
	if len(candles) < period+1 {
		return 0
	}

	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trueRange(candles[i], candles[i-1])
	}
	atr := sum / float64(period)

	multiplier := 1.0 / float64(period)
	for i := period + 1; i < len(candles); i++ {
		tr := trueRange(candles[i], candles[i-1])
		atr = atr*(1-multiplier) + tr*multiplier
	}
	return atr
}

func CalcATRRatio(candles []RestCandle) float64 {
	if len(candles) < 101 {
		return 0
	}
	atr14 := calcATR(candles, 14)
	atr100 := calcATR(candles, 100)
	if atr100 == 0 {
		return 0
	}
	return atr14 / atr100
}

// ADX Calculation
type adxResult struct {
	ADX     float64
	PlusDI  float64
	MinusDI float64
}

func CalcADX(candles []RestCandle, period int) adxResult {
	if len(candles) < period*2 {
		return adxResult{}
	}

	smoothTR := 0.0
	smoothPlusDM := 0.0
	smoothMinusDM := 0.0

	for i := 1; i <= period; i++ {
		tr := trueRange(candles[i], candles[i-1])
		plusDM := candles[i].High - candles[i-1].High
		minusDM := candles[i-1].Low - candles[i].Low

		if plusDM < 0 {
			plusDM = 0
		}
		if minusDM < 0 {
			minusDM = 0
		}
		if plusDM > minusDM {
			minusDM = 0
		} else if minusDM > plusDM {
			plusDM = 0
		} else {
			plusDM = 0
			minusDM = 0
		}

		smoothTR += tr
		smoothPlusDM += plusDM
		smoothMinusDM += minusDM
	}

	multiplier := 1.0 / float64(period)
	var dxValues []float64

	for i := period + 1; i < len(candles); i++ {
		tr := trueRange(candles[i], candles[i-1])
		plusDM := candles[i].High - candles[i-1].High
		minusDM := candles[i-1].Low - candles[i].Low

		if plusDM < 0 {
			plusDM = 0
		}
		if minusDM < 0 {
			minusDM = 0
		}
		if plusDM > minusDM {
			minusDM = 0
		} else if minusDM > plusDM {
			plusDM = 0
		} else {
			plusDM = 0
			minusDM = 0
		}

		smoothTR = smoothTR*(1-multiplier) + tr
		smoothPlusDM = smoothPlusDM*(1-multiplier) + plusDM
		smoothMinusDM = smoothMinusDM*(1-multiplier) + minusDM

		if smoothTR == 0 {
			continue
		}

		plusDI := (smoothPlusDM / smoothTR) * 100
		minusDI := (smoothMinusDM / smoothTR) * 100

		diDiff := math.Abs(plusDI - minusDI)
		diSum := plusDI + minusDI
		if diSum == 0 {
			continue
		}
		dxValues = append(dxValues, (diDiff/diSum)*100)
	}

	if len(dxValues) < period {
		return adxResult{}
	}

	adx := 0.0
	for _, dx := range dxValues[len(dxValues)-period:] {
		adx += dx
	}
	adx /= float64(period)

	plusDI := (smoothPlusDM / smoothTR) * 100
	minusDI := (smoothMinusDM / smoothTR) * 100

	return adxResult{
		ADX:     adx,
		PlusDI:  plusDI,
		MinusDI: minusDI,
	}
}

// BBW Calculation
func CalcBandWidth(candles []RestCandle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	recent := candles[len(candles)-period:]

	sum := 0.0
	for _, c := range recent {
		sum += c.Close
	}
	sma := sum / float64(period)

	variance := 0.0
	for _, c := range recent {
		diff := c.Close - sma
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	upper := sma + 2*stdDev
	lower := sma - 2*stdDev

	if sma == 0 {
		return 0
	}
	return (upper - lower) / sma
}
