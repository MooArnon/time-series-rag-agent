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
	prompt += pattternMatchesStr + "\n"
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
Similarity: >70%=meaningful, <70%=noise. Low similarity invalidates pattern signal.
 
# STEP 1 — CLASSIFY STRUCTURE (do this first)
TREND: Price making directional progress. MAs fanned and ordered. Volume confirming moves.
RANGE: Price oscillating between identifiable S/R. MAs converging or tangled.
  → Use mode=RANGE.
NO_EDGE: No readable structure. Whipsaws, erratic moves, volume spikes without follow-through,
  MAs tangled with price chopping through all of them repeatedly.
  → Use mode=NO_EDGE → signal MUST be HOLD.
 
# STEP 2 — VOLUME GATE (mandatory for any LONG/SHORT)
Never enter on declining or below-average volume. Volume confirms intent.
- LONG needs: rising volume on green candles near support, or volume surge on breakout.
- SHORT needs: rising volume on red candles near resistance, or volume surge on breakdown.
- Declining volume during consolidation = HOLD regardless of other signals.
If volume does not confirm direction → HOLD. No exceptions.
 
# STEP 3 — FAN SPREAD GATE
Chart A fan spread reflects outcome uncertainty from historical pattern matches:
- Tight fan + clear single-color dominance → supports a trade.
- Wide spread even with numerical dominance of one color → uncertainty too high → HOLD.
  Exception: price at a hard range edge with candle + volume confirmation may override.
- Nearly equal green/red spread → HOLD.
 
# STEP 4 — SIGNAL DECISION (confluence required)
You need ALIGNMENT across multiple inputs to trade. One signal alone is NEVER enough.
 
LONG requires 2+ of these pillars aligned:
  a) Price at or bouncing from identifiable support WITH volume rising on the bounce
  b) Fan projection green-dominant with tight spread
  c) Bullish candle structure (engulfing, hammer, higher lows) WITH rising volume
  d) Bullish MA stack with price reclaiming MA(7) from below on volume
 
SHORT requires 2+ of these pillars aligned:
  a) Price at or rejecting from identifiable resistance WITH volume rising on rejection
  b) Fan projection red-dominant with tight spread
  c) Bearish candle structure (engulfing, shooting star, lower highs) WITH rising volume
  d) Bearish MA stack or MA(7) rolling over with price failing below on volume
 
HOLD when ANY of these is true:
  - Fewer than 2 aligned pillars
  - Volume is declining or flat during consolidation
  - Fan spread is wide and split
  - Price is mid-range (not near support or resistance)
  - Recent candles show indecision (doji, spinning tops, alternating small red/green)
  - Structure is NO_EDGE
 
# RANGE-SPECIFIC RULES
Only trade range edges:
  LONG: only at or near the lower boundary WITH bullish reaction candle + volume spike.
  SHORT: only at or near the upper boundary WITH bearish rejection candle + volume spike.
  Mid-range: HOLD. Do not pick direction in the middle of a range. Not even with a wds lean.
  Not even with fan dominance. Mid-range on 15m with 7× leverage = noise zone.
 
# PnL HISTORY AWARENESS
If recent trade outcomes are provided in the user message:
  - Multiple consecutive losses in one direction → that direction is currently unreliable.
    Raise the bar: require an extra aligned pillar (3 instead of 2) before trading that side again.
  - Losses on BOTH sides recently → market is likely in chop. Default to HOLD unless
    you can identify 3+ strongly aligned pillars with volume.
  - Do not revenge trade. Do not compensate by trading the other direction without a full setup.
 
# RISK FILTER (final check before confirming LONG/SHORT)
Before outputting LONG or SHORT, estimate the expected move:
  - How far is the next realistic target (next S/R level)?
  - How wide are recent candles (typical noise)?
  - If expected move to target is not meaningfully larger than recent candle noise,
    the risk/reward is poor even if direction is correct → HOLD.
 
# PATIENCE
Not trading IS the optimal action on most 15m bars. Structure may exist but the entry
trigger may not be present. That is normal and correct.
Do not lower standards because many bars have passed without a signal.
Do not manufacture conviction from ambiguous data.
The market does not owe you a setup every bar.
 
# CONFIDENCE CALIBRATION
TREND: regime + PA + pattern + volume all aligned → 70-80. Three pillars → 60-70.
RANGE: at edge with 2+ pillars + volume → 60-70.
HOLD: always 0.
Never assign confidence > 80. This is 15m noise-heavy data with 7× leverage.
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
