package prefilter_test

import (
	"math"
	"testing"

	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/prefilter"
)

// ---------------------------------------------------------------------------
// Synthetic bar builders
// ---------------------------------------------------------------------------

// trendBars builds n bars in a consistent uptrend.
// Each bar: open = prevClose, close = open + delta, wicks ± wick.
func trendBars(n int, startPrice, delta, wick, vol float64) []exchange.WsRestCandle {
	bars := make([]exchange.WsRestCandle, n)
	price := startPrice
	for i := 0; i < n; i++ {
		open := price
		close := open + delta
		bars[i] = exchange.WsRestCandle{
			Time:   int64(i) * 900,
			Open:   open,
			High:   close + wick,
			Low:    open - wick,
			Close:  close,
			Volume: vol,
		}
		price = close
	}
	return bars
}

// flatBars builds n oscillating bars around basePrice with tiny bodies (chop).
// Bodies alternate ±bodySize; HL span = hlRange.
func flatBars(n int, basePrice, hlRange, bodySize, vol float64) []exchange.WsRestCandle {
	bars := make([]exchange.WsRestCandle, n)
	for i := 0; i < n; i++ {
		dir := float64(1 - 2*(i%2))
		open := basePrice - dir*bodySize*0.5
		close := basePrice + dir*bodySize*0.5
		bars[i] = exchange.WsRestCandle{
			Time:   int64(i) * 900,
			Open:   open,
			High:   basePrice + hlRange*0.5,
			Low:    basePrice - hlRange*0.5,
			Close:  close,
			Volume: vol,
		}
	}
	return bars
}

// setBar replaces one bar in a copy of bars.
func setBar(bars []exchange.WsRestCandle, idx int, b exchange.WsRestCandle) []exchange.WsRestCandle {
	out := make([]exchange.WsRestCandle, len(bars))
	copy(out, bars)
	b.Time = out[idx].Time
	out[idx] = b
	return out
}

// setLast replaces the last bar.
func setLast(bars []exchange.WsRestCandle, open, high, low, close, vol float64) []exchange.WsRestCandle {
	out := make([]exchange.WsRestCandle, len(bars))
	copy(out, bars)
	n := len(out)
	out[n-1] = exchange.WsRestCandle{
		Time: out[n-1].Time, Open: open, High: high, Low: low, Close: close, Volume: vol,
	}
	return out
}

// ---------------------------------------------------------------------------
// Table-driven tests
// ---------------------------------------------------------------------------

func TestRunPrefilter(t *testing.T) {
	tests := []struct {
		name      string
		candles   func() []exchange.WsRestCandle
		threshold float64
		check     func(t *testing.T, r prefilter.Result)
	}{
		{
			// Strong uptrend: 119 bars, consistent +100/bar, large final body, 3× volume.
			// Pure uptrend creates no swing points (price always makes new highs), so
			// srScore = 0. Without S/R: body≈14 + vol=20 + ma=15 + adx=12 = ~61.
			// Lower bound is 55, not the theoretical 65 that would need S/R.
			name: "strong uptrend",
			candles: func() []exchange.WsRestCandle {
				bars := trendBars(119, 50_000, 100, 30, 1000)
				// Last bar: body ≈ 1.5×ATR (≈240 >> ATR≈167), volume = 3×
				last := bars[117].Close
				return setLast(bars, last, last+270, last-30, last+240, 3000)
			},
			threshold: 35,
			check: func(t *testing.T, r prefilter.Result) {
				if r.Score < 55 {
					t.Errorf("strong uptrend: want score >= 55, got %.1f "+
						"(body=%.1f vol=%.1f ma=%.1f adx=%.1f sr=%.1f chop=%.1f stale=%.1f)",
						r.Score, r.BodyScore, r.VolumeScore, r.MAScore,
						r.ADXBonus, r.SRProximity, r.ChopPenalty, r.StalePenalty)
				}
				if !r.PassThreshold {
					t.Errorf("strong uptrend: expected PassThreshold=true")
				}
				// Verify individual drivers
				if r.MAScore < 14 {
					t.Errorf("strong uptrend: MA should be fully aligned, got MAScore=%.1f", r.MAScore)
				}
				if r.ADXBonus < 10 {
					t.Errorf("strong uptrend: ADX should be >25 in strong trend, got ADXBonus=%.1f", r.ADXBonus)
				}
			},
		},
		{
			// Range mid-state: 120 oscillating bars, tiny bodies, MAs tangled, no surge.
			// Chop + stale penalties dominate; score should be near 0.
			name: "range mid-state",
			candles: func() []exchange.WsRestCandle {
				return flatBars(120, 50_000, 200, 5, 1000)
			},
			threshold: 35,
			check: func(t *testing.T, r prefilter.Result) {
				if r.Score >= 25 {
					t.Errorf("range mid-state: want score < 25, got %.1f", r.Score)
				}
				if r.PassThreshold {
					t.Errorf("range mid-state: expected PassThreshold=false")
				}
			},
		},
		{
			// Range edge with reaction: price bounces from a clear support level.
			// Two explicit S/R landmarks are injected:
			//   bars[113]: deep dip → swing low at 49600
			//   bars[115]: spike high → swing high at 50300
			// Last bar: opens at support (49600), closes near resistance (50220).
			//   Distance to nearest SR = |50220 - 50300| = 80 → srScore ≈ 16
			//   body = 620 >> ATR → bodyScore = 15 (max)
			//   volume = 2500 (2.5× avg) → volScore = 20 (max)
			//   chop: 10-bar span = 700 > 2×ATR ≈ 502 → chopPenalty = 0
			// Expected: 16 + 15 + 20 = 51, well in 35–65.
			name: "range edge with reaction",
			candles: func() []exchange.WsRestCandle {
				bars := flatBars(119, 50_000, 200, 10, 1000)
				// Inject swing low at index 113 (neighbors 112 and 114 have low=49900)
				bars = setBar(bars, 113, exchange.WsRestCandle{
					Open: 49_990, High: 50_100, Low: 49_600, Close: 50_010, Volume: 900,
				})
				// Inject swing high at index 115 (neighbors 114 and 116 have high=50100)
				bars = setBar(bars, 115, exchange.WsRestCandle{
					Open: 49_990, High: 50_300, Low: 49_900, Close: 50_010, Volume: 950,
				})
				// Last bar: open at prior support, close near prior resistance
				return setLast(bars,
					49_600, // open  — at support
					50_280, // high
					49_560, // low
					50_220, // close — 80 below swing-high resistance (50300)
					2500,   // volume = 2.5× avg
				)
			},
			threshold: 35,
			check: func(t *testing.T, r prefilter.Result) {
				if r.Score < 35 || r.Score > 70 {
					t.Errorf("range edge: want score 35–70, got %.1f "+
						"(sr=%.1f body=%.1f vol=%.1f ma=%.1f chop=%.1f stale=%.1f)",
						r.Score, r.SRProximity, r.BodyScore, r.VolumeScore,
						r.MAScore, r.ChopPenalty, r.StalePenalty)
				}
				if !r.PassThreshold {
					t.Errorf("range edge: expected PassThreshold=true at threshold=35")
				}
				if r.SRProximity < 10 {
					t.Errorf("range edge: S/R proximity should be significant, got %.1f", r.SRProximity)
				}
			},
		},
		{
			// Pure chop: 30 bars with ATR≈200, last 10 bars overridden to a 30-pt
			// range (≈0.15×ATR) with hairline bodies. Maximum chop + stale penalties.
			name: "pure chop",
			candles: func() []exchange.WsRestCandle {
				bars := flatBars(30, 50_000, 200, 2, 1000)
				for i := 20; i < 30; i++ {
					dir := float64(1 - 2*(i%2))
					bars[i] = exchange.WsRestCandle{
						Time:   bars[i].Time,
						Open:   50_000 - dir,
						High:   50_015,
						Low:    49_985,
						Close:  50_000 + dir,
						Volume: 1000,
					}
				}
				return bars
			},
			threshold: 35,
			check: func(t *testing.T, r prefilter.Result) {
				if r.Score >= 15 {
					t.Errorf("pure chop: want score < 15, got %.1f "+
						"(chop=%.1f stale=%.1f)", r.Score, r.ChopPenalty, r.StalePenalty)
				}
				if r.PassThreshold {
					t.Errorf("pure chop: expected PassThreshold=false")
				}
			},
		},
		{
			// High ATR, no structure: 120 bars alternating +300/-300, wick±100.
			// Large ATR (≈500) but MAs are completely tangled → MAScore = 0.
			// No volume surge, no ADX. Score should be low (< 45).
			name: "high ATR no structure",
			candles: func() []exchange.WsRestCandle {
				bars := make([]exchange.WsRestCandle, 120)
				price := 50_000.0
				for i := 0; i < 120; i++ {
					dir := float64(1 - 2*(i%2))
					open := price
					close := open + dir*300
					bars[i] = exchange.WsRestCandle{
						Time:   int64(i) * 900,
						Open:   open,
						High:   math.Max(open, close) + 100,
						Low:    math.Min(open, close) - 100,
						Close:  close,
						Volume: 1000,
					}
					price = close
				}
				return bars
			},
			threshold: 35,
			check: func(t *testing.T, r prefilter.Result) {
				if r.Score >= 45 {
					t.Errorf("high ATR no structure: want score < 45, got %.1f", r.Score)
				}
				if r.MAScore > 0 {
					t.Errorf("high ATR no structure: MAs should be tangled, got MAScore=%.1f", r.MAScore)
				}
			},
		},
		{
			// Edge case: fewer than 21 candles → immediate SKIP with "insufficient data".
			name: "insufficient data",
			candles: func() []exchange.WsRestCandle {
				return trendBars(10, 50_000, 100, 30, 1000)
			},
			threshold: 35,
			check: func(t *testing.T, r prefilter.Result) {
				if r.Score != 0 {
					t.Errorf("insufficient data: want Score=0, got %.1f", r.Score)
				}
				if r.PassThreshold {
					t.Errorf("insufficient data: want PassThreshold=false")
				}
				if r.SkipReason != "insufficient data" {
					t.Errorf("insufficient data: want SkipReason='insufficient data', got %q", r.SkipReason)
				}
			},
		},
		{
			// Configurable threshold — same candles (≈50 score) pass at low threshold.
			name: "threshold configurable — pass at low threshold",
			candles: func() []exchange.WsRestCandle {
				// Reuse range-edge data (score ≈ 40-65, well above 10).
				bars := flatBars(119, 50_000, 200, 10, 1000)
				bars = setBar(bars, 113, exchange.WsRestCandle{
					Open: 49_990, High: 50_100, Low: 49_600, Close: 50_010, Volume: 900,
				})
				bars = setBar(bars, 115, exchange.WsRestCandle{
					Open: 49_990, High: 50_300, Low: 49_900, Close: 50_010, Volume: 950,
				})
				return setLast(bars, 49_600, 50_280, 49_560, 50_220, 2500)
			},
			threshold: 10,
			check: func(t *testing.T, r prefilter.Result) {
				if !r.PassThreshold {
					t.Errorf("threshold=10: expected PassThreshold=true, got score=%.1f", r.Score)
				}
			},
		},
		{
			// Configurable threshold — same candles fail at high threshold.
			name: "threshold configurable — fail at high threshold",
			candles: func() []exchange.WsRestCandle {
				bars := flatBars(119, 50_000, 200, 10, 1000)
				bars = setBar(bars, 113, exchange.WsRestCandle{
					Open: 49_990, High: 50_100, Low: 49_600, Close: 50_010, Volume: 900,
				})
				bars = setBar(bars, 115, exchange.WsRestCandle{
					Open: 49_990, High: 50_300, Low: 49_900, Close: 50_010, Volume: 950,
				})
				return setLast(bars, 49_600, 50_280, 49_560, 50_220, 2500)
			},
			threshold: 80,
			check: func(t *testing.T, r prefilter.Result) {
				if r.PassThreshold {
					t.Errorf("threshold=80: expected PassThreshold=false, got score=%.1f", r.Score)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := prefilter.RunPrefilter(prefilter.Input{
				Candles:   tc.candles(),
				Threshold: tc.threshold,
			})
			tc.check(t, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests for individual helpers
// ---------------------------------------------------------------------------

func TestComputeATR(t *testing.T) {
	// 21 identical bars: Open=1000, High=1090, Low=1000, Close=1050.
	// For i > 0: TR = max(H-L=90, |H-prevC|=|1090-1050|=40, |L-prevC|=|1000-1050|=50) = 90
	bars := make([]exchange.WsRestCandle, 21)
	for i := 0; i < 21; i++ {
		bars[i] = exchange.WsRestCandle{Open: 1000, High: 1090, Low: 1000, Close: 1050}
	}
	if atr := prefilter.ComputeATR(bars, 20); math.Abs(atr-90) > 0.01 {
		t.Errorf("ComputeATR: want 90.00, got %.2f", atr)
	}
	if got := prefilter.ComputeATR(bars[:5], 20); got != 0 {
		t.Errorf("ComputeATR insufficient bars: want 0, got %.2f", got)
	}
}

func TestComputeSMA(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	// last 5 elements: 6,7,8,9,10 → avg = 8
	if got := prefilter.ComputeSMA(vals, 5); math.Abs(got-8) > 0.001 {
		t.Errorf("ComputeSMA last-5: want 8.000, got %.3f", got)
	}
	if got := prefilter.ComputeSMA(vals[:3], 5); got != 0 {
		t.Errorf("ComputeSMA insufficient: want 0, got %.3f", got)
	}
}

func TestNearestSRDistance(t *testing.T) {
	levels := []float64{100, 200, 300}
	if d := prefilter.NearestSRDistance(195, levels); math.Abs(d-5) > 0.001 {
		t.Errorf("NearestSRDistance 195→200: want 5, got %.3f", d)
	}
	if d := prefilter.NearestSRDistance(150, nil); d != math.MaxFloat64 {
		t.Errorf("NearestSRDistance empty: want MaxFloat64, got %v", d)
	}
}

func TestComputeMAAlignment(t *testing.T) {
	// 110-bar uptrend: MA7 >> MA25 >> MA99 → alignment ≥ 0.9
	up := trendBars(110, 50_000, 200, 50, 1000)
	atr := prefilter.ComputeATR(up, 20)
	if score := prefilter.ComputeMAAlignment(up, atr); score < 0.9 {
		t.Errorf("uptrend MA alignment: want ≥0.9, got %.2f", score)
	}

	// 110-bar oscillation: MAs tangled → 0
	osc := flatBars(110, 50_000, 200, 5, 1000)
	atr2 := prefilter.ComputeATR(osc, 20)
	if score := prefilter.ComputeMAAlignment(osc, atr2); score != 0 {
		t.Errorf("oscillating MA alignment: want 0, got %.2f", score)
	}
}

func TestComputeChopFactor(t *testing.T) {
	atr := 100.0

	// 10-bar range = 50 < 2×100=200 → chopFactor = 1 - 50/200 = 0.75
	tight := make([]exchange.WsRestCandle, 10)
	for i := range tight {
		tight[i] = exchange.WsRestCandle{High: 50_025, Low: 49_975}
	}
	if cf := prefilter.ComputeChopFactor(tight, atr); math.Abs(cf-0.75) > 0.01 {
		t.Errorf("tight: want chopFactor=0.75, got %.3f", cf)
	}

	// 10-bar range = 300 > 200 → chopFactor = 0
	wide := make([]exchange.WsRestCandle, 10)
	for i := range wide {
		wide[i] = exchange.WsRestCandle{High: 50_150, Low: 49_850}
	}
	if cf := prefilter.ComputeChopFactor(wide, atr); cf != 0 {
		t.Errorf("wide: want chopFactor=0, got %.3f", cf)
	}
}
