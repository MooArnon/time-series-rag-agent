package llm

import (
	"fmt"
	"strings"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/trade"
)

func FormatUserPrompt(
	pnlData []trade.PositionHistory,
	regime4h exchange.RegimeResult,
	regime1d exchange.RegimeResult,
	matches []HistoricalDetail,
	matches1H []HistoricalDetail,
	pnlSummary float64,
) string {
	// 1. Format the PnL data into a string that can be included in the prompt
	pnlStr := "# PnL Table:\n"
	pnlStr += "Position Open Time | RealizedPnL | Signal Side \n"
	pnlStr += "-------------------|-------------|-------------\n"
	for _, data := range pnlData {
		pnlStr += fmt.Sprintf("%s | %.2f | %s\n",
			data.OpenTime, data.RealizedPnL, data.PositionSide)
	}
	pnlStr += "-------------------|---------|-------------\n"

	// 2. Create the final prompt by embedding the formatted PnL string
	basePromt := ""

	// Adding PnL table data
	prompt := basePromt + pnlStr

	// Adding PnL summary data
	prompt += "\n# Daily PnL SUMMARY:\n" + fmt.Sprint(pnlSummary) + "\n\n"

	// Adding regime context
	regimePromt := "# REGIME CONTEXT:\n"
	regimePromt += "Interval | Regime | Direction | ADX | PlusDI | MinusDI | ATRRatio | BandWidth\n"
	regimePromt += "---------|--------|-----------|-----|--------|---------|----------|---------|\n"

	if regime4h != (exchange.RegimeResult{}) {
		regimePromt += fmt.Sprintf("%s | %s | %s | %.2f | %.2f | %.2f | %.2f | %.2f\n",
			"4H", regime4h.Regime, regime4h.Direction, regime4h.ADX, regime4h.PlusDI, regime4h.MinusDI, regime4h.ATRRatio, regime4h.BandWidth)
	}
	if regime1d != (exchange.RegimeResult{}) {
		regimePromt += fmt.Sprintf("%s |%s | %s | %.2f | %.2f | %.2f | %.2f | %.2f\n",
			"1D", regime1d.Regime, regime1d.Direction, regime1d.ADX, regime1d.PlusDI, regime1d.MinusDI, regime1d.ATRRatio, regime1d.BandWidth)
	}
	regimePromt += "---------|--------|-----------|-----|--------|---------|----------|---------|\n"

	prompt += regimePromt

	// Adding historical pattern matches
	pattternMatchesStr := FormatPatternMatches(matches)
	prompt += "\nMain timeframe 15 minutes"
	prompt += pattternMatchesStr + "\n"

	prompt += "\nAdditional 1H timeframe for further consideration"
	additionalMatchesStr1H := FormatPatternMatches(matches1H)
	prompt += additionalMatchesStr1H + "\n"

	prompt += "Produce your signal."
	return prompt
}

func FormatPatternMatches(matches []HistoricalDetail) string {
	if len(matches) == 0 {
		return "\n# HISTORICAL PATTERN MATCHES:\nNo matches found.\n"
	}

	matches = matches[1:]

	totalDown, totalUp := 0, 0
	highDown, highUp := 0, 0
	midDown, midUp := 0, 0
	bestSim := 0.0
	bestOutcome := ""

	for _, m := range matches {
		var sim float64
		fmt.Sscanf(strings.TrimSuffix(m.Similarity, "%"), "%f", &sim)

		if sim > bestSim {
			bestSim = sim
			bestOutcome = m.TrendOutcome
		}

		if m.TrendOutcome == "DOWN" {
			totalDown++
			if sim > 90 {
				highDown++
			} else if sim >= 60 {
				midDown++
			}
		} else {
			totalUp++
			if sim > 90 {
				highUp++
			} else if sim >= 60 {
				midUp++
			}
		}
	}

	top := matches
	if len(top) > 5 {
		top = matches[:5]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n# HISTORICAL PATTERN MATCHES (%d matches):\n", len(matches)))
	sb.WriteString(fmt.Sprintf("DOWN: %d | UP: %d\n", totalDown, totalUp))
	sb.WriteString(fmt.Sprintf("Similarity > 90%%:  DOWN: %d, UP: %d  (best match %.1f%% → %s)\n",
		highDown, highUp, bestSim, bestOutcome))
	sb.WriteString(fmt.Sprintf("Similarity 60-90%%: DOWN: %d, UP: %d\n", midDown, midUp))
	sb.WriteString(fmt.Sprintf("Top %d closest matches:\n", len(top)))

	for _, m := range top {
		sb.WriteString(fmt.Sprintf("%s | slope: %s | %-4s | return: %s | sim: %s\n",
			m.Time, m.TrendSlope, m.TrendOutcome, m.ImmediateReturn, m.Similarity,
		))
	}

	return sb.String()
}

func GetBasePrompt() string {
	return `Quant analyst. Binance Futures BTCUSDT Perp, 15m bars, 7× isolated leverage. Return one JSON signal.

# CHARTS
A (Pattern): BLACK=current price path. GREEN=historically UP outcomes. RED=historically DOWN outcomes. Dashed=projected.
  Read: black line slope into divider, green vs red fan dominance, fan spread width (tight=conviction, wide=uncertainty).
B (Price Action): Candles + volume bars. MA(7) orange, MA(25) purple, MA(99) pink.
  Read: MA stack order and spacing, last 3-5 candle bodies and wicks, volume trend vs recent bars, key S/R levels.

# DATA
Regime: ADX>40=strong trend, 20-40=moderate, <20=ranging. +DI>-DI=bull, -DI>+DI=bear.
Pattern: wds[-1,+1]. Pre-computed directional lean. >+0.15=UP bias, <-0.15=DOWN bias. Near zero=no edge.
Similarity: >80%=strong, 70-80%=moderate, <70%=weak/noise. Low similarity reduces confidence but does not auto-invalidate.

# STEP 1 — CLASSIFY STRUCTURE (do this first)
TREND: Price making directional progress. MAs fanned and ordered. Volume confirming moves.
RANGE: Price oscillating between identifiable S/R. MAs converging or tangled. → Use mode=RANGE.
BREAKOUT: Volume surge (2x+ recent average) pushing price beyond an established range boundary.
  MAs may still be tangled from prior range, but the move is decisive. → Use mode=BREAKOUT.
NO_EDGE: No readable structure. Whipsaws, erratic moves, volume spikes without follow-through,
  MAs tangled with price chopping through all of them repeatedly AND no identifiable S/R boundaries.
  → Use mode=NO_EDGE → signal MUST be HOLD.

NOTE: "MAs tangled" alone does NOT mean NO_EDGE. MAs tangle during ranges and before breakouts.
  NO_EDGE requires tangled MAs + no identifiable S/R + no volume pattern + no candle structure.

# STEP 2 — VOLUME CHECK (contextual, not binary)
Volume confirms intent, but must be evaluated contextually:
- Compare volume to recent 10-20 bars, not to the highest bar on the chart.
- Naturally low-volume periods (overnight, weekends) have lower baselines — a relative uptick matters.
- LONG needs: volume uptick on green candles near support, or volume surge on breakout.
- SHORT needs: volume uptick on red candles near resistance, or volume surge on breakdown.
- Flat/declining volume during tight consolidation reduces confidence by 10-15 but does NOT auto-HOLD
  if other signals are strong.
- Volume surge on a single candle with follow-through candles = valid even if subsequent bars decline
  (the surge WAS the confirmation; declining after means digestion, not invalidation).

# STEP 3 — FAN SPREAD CHECK (weighted, not binary)
Chart A fan spread reflects historical outcome uncertainty:
- Tight fan + clear single-color dominance → strong support for a trade (+10 confidence).
- Moderate spread with one-color numerical dominance → mild support (+5 confidence).
- Wide spread with clear one-color dominance → neutral (do not add or subtract).
- Wide spread with nearly equal green/red → reduces confidence by 10-15, but does NOT auto-HOLD.
  Exception: price at a range edge with candle + volume confirmation can override.
- Fan spread is ONE input among several. It should never single-handedly block a trade
  when candle structure, MA behavior, and relative volume align.

# STEP 4 — SIGNAL DECISION (confluence-based scoring)

Instead of counting hard pillars, score the setup:

Each factor adds to a running confidence score:
  a) Price at or near S/R edge (within ~30% of range width from boundary)  → +15
  b) Fan projection with clear single-color dominance                      → +10
  c) Confirming candle structure (engulfing, hammer, rejection wick, etc.) → +10
  d) MA alignment supporting direction (stack order or crossover)          → +10
  e) Relative volume uptick confirming the move                            → +10
  f) ADX regime alignment (trending regime + direction match)              → +5
  g) wds lean > 0.15 in trade direction with similarity > 75%             → +5

Deductions:
  h) Wide fan spread with nearly equal distribution                        → -10
  i) Flat/declining volume during consolidation                            → -10
  j) Price at mid-range (middle 40% of identified range)                   → -10
  k) Conflicting signals across timeframes (15m vs 1H disagree)            → -5

LONG: Score >= 25 → signal LONG. Confidence = min(score, 80).
SHORT: Score >= 25 → signal SHORT. Confidence = min(score, 80).
HOLD: Score < 25 for both directions. Confidence = 0.

BREAKOUT MODE (special):
When volume surges 2x+ above recent average AND price breaks beyond a range boundary:
  - If break is UP with strong green candle: score starts at 30 (already qualifies).
    Deduct only if candle shows long upper wick (exhaustion) or regime strongly bearish.
  - If break is DOWN with strong red candle: score starts at 30 (already qualifies).
    Deduct only if candle shows long lower wick (exhaustion) or regime strongly bullish.
  - Do NOT chase breakouts after they've already moved 1%+ from the breakout point.

# RANGE-SPECIFIC RULES
Trade near range edges, not dead center:
  "Near edge" = within ~30% of range width from the boundary.
  LONG: price in lower 30% of range WITH supporting candle + relative volume uptick.
  SHORT: price in upper 30% of range WITH rejection candle + relative volume uptick.
  Mid-range (middle 40%): Apply -10 deduction. Can still trade if other factors score high enough.
  Do not automatically HOLD in mid-range — sometimes MAs cross or candles form patterns
  mid-range that are actionable.

# RISK FILTER (final check before confirming LONG/SHORT)
Before outputting LONG or SHORT:
  - How far is the next realistic target (next S/R level)?
  - How wide are recent candles (typical noise)?
  - If expected move to target is smaller than 1.5× recent candle noise → HOLD.
  - At 7× leverage, even small wins compound. Don't require massive targets.
    A 0.15% move = ~1% at 7×. That is a valid target.

# PATIENCE CALIBRATION
Not every bar should trade, but not every bar should HOLD either.
A reasonable target is 3-8 trades per 24-hour period on 15m bars.
If you've been outputting HOLD for 20+ consecutive bars, recalibrate:
  - Are you being too strict on fan spread? (It's a ranging market — fans WILL be wide.)
  - Are you requiring perfection across every input? (2-3 strong signals are enough.)
  - Is there an actionable setup you're discounting because ONE input is weak?
The market does not owe you a perfect setup, and waiting for perfection is also a form of error.

# CONFIDENCE CALIBRATION
Score-based from Step 4, capped at 80.
TREND: All factors aligned → 70-80. Most factors → 60-70.
RANGE: At edge with 3+ supporting factors → 60-70. Mid-range with strong factors → 50-60.
BREAKOUT: Volume-confirmed break → 65-75.
HOLD: always 0.

# OUTPUT FORMAT
Respond with ONLY a single JSON object. No preamble, no markdown, no explanation before or after.
Do not include your reasoning outside the JSON.
Put your analysis INSIDE the JSON fields (synthesis, pattern_read, price_action_read, etc.).
The very first character of your response must be { and the very last must be }.
`
}

func GetPromptConstraint() string {
	return `# OUTPUT — exactly one JSON, no markdown, no backticks.
 
{
  "mode": "TREND" | "RANGE" | "NO_EDGE",
  "signal": "LONG" | "SHORT" | "HOLD",
  "confidence": <int 0-100, round to 5>,
  "regime_read": "<1 sentence>",
  "pattern_read": "<1 sentence>",
  "price_action_read": "<1 sentence with price levels>",
  "synthesis": "<2 sentences max>",
  "risk_note": "<1 sentence>",
  "invalidation": "<price level or condition>"
}
 
All fields non-empty. Invalidation must have a number.
HOLD invalidation = what would trigger LONG or SHORT.
Only reference prices visible in Chart B.
`
}
