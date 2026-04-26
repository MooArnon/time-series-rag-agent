package prefilter

import (
	"fmt"
	"log/slog"
	"math"
	"strings"

	"time-series-rag-agent/internal/exchange"
)

const DefaultThreshold = 35.0

// Input holds everything the pre-filter needs to evaluate one bar.
type Input struct {
	Candles   []exchange.WsRestCandle
	Threshold float64 // 0 uses DefaultThreshold
}

// Result contains the pass/fail decision, the composite score, and every
// component so callers can log or inspect individual contributions.
type Result struct {
	Score         float64
	PassThreshold bool
	SkipReason    string

	// Per-component breakdown (positive = additive, negative = deductive)
	ATR          float64
	SRProximity  float64
	BodyScore    float64
	VolumeScore  float64
	MAScore      float64
	ADXBonus     float64
	ChopPenalty  float64
	StalePenalty float64
}

// RunPrefilter evaluates the pre-filter gate and emits a structured log line.
func RunPrefilter(input Input) Result {
	threshold := input.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}

	if len(input.Candles) < 21 {
		r := Result{SkipReason: "insufficient data"}
		logResult(r, threshold)
		return r
	}

	atr := ComputeATR(input.Candles, 20)
	if atr == 0 {
		r := Result{SkipReason: "insufficient data"}
		logResult(r, threshold)
		return r
	}

	last := input.Candles[len(input.Candles)-1]
	price := last.Close

	// a) S/R proximity — max +20
	srLevels := computeSRLevels(input.Candles, 50)
	distToSR := NearestSRDistance(price, srLevels)
	srFactor := math.Max(0, 1-distToSR/(2*atr))
	srScore := 20 * srFactor

	// b) Candle body strength — max +15
	bodyATU := math.Abs(last.Close-last.Open) / atr
	bodyScore := math.Min(bodyATU/1.5, 1.0) * 15

	// c) Volume surge — max +20 (only counts if > average)
	avgVol := computeAvgVolume(input.Candles, 20)
	volScore := 0.0
	if avgVol > 0 {
		volRatio := last.Volume / avgVol
		if volRatio > 1.0 {
			volScore = math.Min((volRatio-1.0)/1.5, 1.0) * 20
		}
	}

	// d) MA alignment — max +15
	maAlignment := ComputeMAAlignment(input.Candles, atr)
	maScore := 15 * maAlignment

	// e) ADX regime bonus — max +12
	adx := ComputeADX(input.Candles, 14)
	adxBonus := 0.0
	if adx > 40 {
		adxBonus = 12
	} else if adx > 25 {
		adxBonus = 10
	}

	// f) Chop penalty — max -15
	chopFactor := ComputeChopFactor(input.Candles, atr)
	chopPenalty := -15 * chopFactor

	// g) Stale bar penalty — max -10
	stalePenalty := 0.0
	if isStale(input.Candles, atr) {
		stalePenalty = -10
	}

	raw := srScore + bodyScore + volScore + maScore + adxBonus + chopPenalty + stalePenalty
	score := math.Max(0, math.Min(100, raw))
	pass := score >= threshold

	skipReason := ""
	if !pass {
		skipReason = buildSkipReason(chopFactor, volScore, maAlignment, stalePenalty)
	}

	result := Result{
		Score:         score,
		PassThreshold: pass,
		SkipReason:    skipReason,
		ATR:           atr,
		SRProximity:   srScore,
		BodyScore:     bodyScore,
		VolumeScore:   volScore,
		MAScore:       maScore,
		ADXBonus:      adxBonus,
		ChopPenalty:   chopPenalty,
		StalePenalty:  stalePenalty,
	}
	logResult(result, threshold)
	return result
}

func logResult(r Result, threshold float64) {
	verdict := "PASS"
	if !r.PassThreshold {
		verdict = "SKIP (" + r.SkipReason + ")"
	}
	slog.Info("[Prefilter] evaluation",
		"score", fmt.Sprintf("%.1f", r.Score),
		"threshold", threshold,
		"atr", fmt.Sprintf("%.2f", r.ATR),
		"sr_prox", fmt.Sprintf("%.1f", r.SRProximity),
		"body", fmt.Sprintf("%.1f", r.BodyScore),
		"vol", fmt.Sprintf("%.1f", r.VolumeScore),
		"ma", fmt.Sprintf("%.1f", r.MAScore),
		"adx", fmt.Sprintf("%.1f", r.ADXBonus),
		"chop", fmt.Sprintf("%.1f", r.ChopPenalty),
		"stale", fmt.Sprintf("%.1f", r.StalePenalty),
		"verdict", verdict,
	)
}

func buildSkipReason(chopFactor, volScore, maAlignment, stalePenalty float64) string {
	var parts []string
	if chopFactor > 0.5 {
		parts = append(parts, "chop")
	}
	if volScore == 0 {
		parts = append(parts, "low volume")
	}
	if maAlignment == 0 {
		parts = append(parts, "tangled MAs")
	}
	if stalePenalty < 0 {
		parts = append(parts, "stale bars")
	}
	if len(parts) == 0 {
		return "low confluence"
	}
	return strings.Join(parts, " + ")
}

// --- Exported helpers (also used in tests) ---

// ComputeATR computes a simple Average True Range over the last period bars.
// Requires at least period+1 candles; returns 0 otherwise.
func ComputeATR(bars []exchange.WsRestCandle, period int) float64 {
	if len(bars) < period+1 {
		return 0
	}
	n := len(bars)
	var sum float64
	for i := n - period; i < n; i++ {
		hl := bars[i].High - bars[i].Low
		hc := math.Abs(bars[i].High - bars[i-1].Close)
		lc := math.Abs(bars[i].Low - bars[i-1].Close)
		sum += math.Max(hl, math.Max(hc, lc))
	}
	return sum / float64(period)
}

// ComputeSMA computes a simple moving average of the last period values.
func ComputeSMA(values []float64, period int) float64 {
	if len(values) < period {
		return 0
	}
	n := len(values)
	var sum float64
	for i := n - period; i < n; i++ {
		sum += values[i]
	}
	return sum / float64(period)
}

// computeAvgVolume returns the mean volume of the 20 bars before the last one,
// so the current bar's volume can be compared against historical baseline.
func computeAvgVolume(bars []exchange.WsRestCandle, period int) float64 {
	if len(bars) < period+1 {
		return 0
	}
	n := len(bars)
	var sum float64
	for i := n - 1 - period; i < n-1; i++ {
		sum += bars[i].Volume
	}
	return sum / float64(period)
}

// computeSRLevels finds swing-high and swing-low price levels within the last
// lookback bars using a simple 1-bar pivot rule.
func computeSRLevels(bars []exchange.WsRestCandle, lookback int) []float64 {
	n := len(bars)
	start := n - lookback
	if start < 1 {
		start = 1
	}
	end := n - 1 // exclude last bar so we always have bars[i+1]

	var levels []float64
	for i := start; i < end; i++ {
		if bars[i].High > bars[i-1].High && bars[i].High > bars[i+1].High {
			levels = append(levels, bars[i].High)
		}
		if bars[i].Low < bars[i-1].Low && bars[i].Low < bars[i+1].Low {
			levels = append(levels, bars[i].Low)
		}
	}
	return levels
}

// NearestSRDistance returns the absolute distance from price to the closest
// S/R level. Returns math.MaxFloat64 when no levels are found.
func NearestSRDistance(price float64, levels []float64) float64 {
	if len(levels) == 0 {
		return math.MaxFloat64
	}
	minDist := math.MaxFloat64
	for _, lvl := range levels {
		if d := math.Abs(price - lvl); d < minDist {
			minDist = d
		}
	}
	return minDist
}

// ComputeMAAlignment returns a 0–1 score for how cleanly MA7/MA25/MA99 are
// fanned in the same direction. Returns 0 if any adjacent pair is tangled
// (gap < 0.1×ATR).
func ComputeMAAlignment(bars []exchange.WsRestCandle, atr float64) float64 {
	if len(bars) < 99 {
		return 0
	}
	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.Close
	}
	ma7 := ComputeSMA(closes, 7)
	ma25 := ComputeSMA(closes, 25)
	ma99 := ComputeSMA(closes, 99)

	tangledThresh := 0.1 * atr
	fanThresh := 0.3 * atr

	if math.Abs(ma7-ma25) < tangledThresh || math.Abs(ma25-ma99) < tangledThresh {
		return 0
	}

	// Count pairs that are aligned AND fanned (> fanThresh) in either direction.
	bullPairs := 0
	if ma7-ma25 > fanThresh {
		bullPairs++
	}
	if ma25-ma99 > fanThresh {
		bullPairs++
	}
	bearPairs := 0
	if ma25-ma7 > fanThresh {
		bearPairs++
	}
	if ma99-ma25 > fanThresh {
		bearPairs++
	}

	best := bullPairs
	if bearPairs > best {
		best = bearPairs
	}
	return float64(best) / 2.0
}

// ComputeChopFactor returns a 0–1 value representing consolidation tightness
// over the last 10 bars, relative to 2×ATR. 0 = no chop, 1 = maximum chop.
func ComputeChopFactor(bars []exchange.WsRestCandle, atr float64) float64 {
	if len(bars) < 10 {
		return 0
	}
	n := len(bars)
	hi := bars[n-10].High
	lo := bars[n-10].Low
	for i := n - 9; i < n; i++ {
		if bars[i].High > hi {
			hi = bars[i].High
		}
		if bars[i].Low < lo {
			lo = bars[i].Low
		}
	}
	span := hi - lo
	threshold := 2 * atr
	if span >= threshold {
		return 0
	}
	return 1 - span/threshold
}

// isStale returns true when the last 5 bars all have a body smaller than
// 0.3×ATR (no decisive candle in the window).
func isStale(bars []exchange.WsRestCandle, atr float64) bool {
	if len(bars) < 5 {
		return false
	}
	bodyThresh := 0.3 * atr
	n := len(bars)
	for i := n - 5; i < n; i++ {
		if math.Abs(bars[i].Close-bars[i].Open) >= bodyThresh {
			return false
		}
	}
	return true
}

// ComputeADX computes ADX using Wilder's smoothing method.
// Returns 0 when there are insufficient bars (< 2*period+1).
func ComputeADX(bars []exchange.WsRestCandle, period int) float64 {
	if len(bars) < 2*period+1 {
		return 0
	}
	n := len(bars)

	type raw struct{ tr, plusDM, minusDM float64 }
	raws := make([]raw, n-1)
	for i := 1; i < n; i++ {
		hl := bars[i].High - bars[i].Low
		hc := math.Abs(bars[i].High - bars[i-1].Close)
		lc := math.Abs(bars[i].Low - bars[i-1].Close)
		tr := math.Max(hl, math.Max(hc, lc))

		upMove := bars[i].High - bars[i-1].High
		downMove := bars[i-1].Low - bars[i].Low

		var plusDM, minusDM float64
		if upMove > downMove && upMove > 0 {
			plusDM = upMove
		} else if downMove > upMove && downMove > 0 {
			minusDM = downMove
		}
		raws[i-1] = raw{tr, plusDM, minusDM}
	}

	// Seed Wilder's smoothing with the sum of the first period values.
	smTR, smPlus, smMinus := 0.0, 0.0, 0.0
	for i := 0; i < period; i++ {
		smTR += raws[i].tr
		smPlus += raws[i].plusDM
		smMinus += raws[i].minusDM
	}

	plusDI, minusDI, dx := diAndDX(smTR, smPlus, smMinus)
	_ = plusDI
	_ = minusDI
	adx := dx

	fp := float64(period)
	for i := period; i < len(raws); i++ {
		smTR = smTR - smTR/fp + raws[i].tr
		smPlus = smPlus - smPlus/fp + raws[i].plusDM
		smMinus = smMinus - smMinus/fp + raws[i].minusDM

		_, _, dx = diAndDX(smTR, smPlus, smMinus)
		adx = (adx*(fp-1) + dx) / fp
	}
	return adx
}

func diAndDX(smTR, smPlus, smMinus float64) (plusDI, minusDI, dx float64) {
	if smTR == 0 {
		return 0, 0, 0
	}
	plusDI = 100 * smPlus / smTR
	minusDI = 100 * smMinus / smTR
	diSum := plusDI + minusDI
	if diSum == 0 {
		return plusDI, minusDI, 0
	}
	dx = 100 * math.Abs(plusDI-minusDI) / diSum
	return
}
