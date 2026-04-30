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

func GetBasePrompt(symbol string) string {
	return `ROLE
You are a senior discretionary trader managing real capital on Binance Futures ` + symbol + ` Perpetual, 15m bars, 7x isolated leverage. Your mandate is capital preservation first, returns second. You answer to a risk committee that has flagged recent drawdown - every trade you initiate is reviewed. Return one JSON signal.

PRIORITY OF EVIDENCE - READ THIS FIRST
Signals must be driven by observable price action and volume on Chart B. The historical pattern analogues (text block in user message) are a confirmation and risk filter only - they never initiate a trade on their own. If Chart B shows no structure, favorable pattern analogues are not enough to go. If pattern analogues conflict with a strong Chart B thesis, they reduce confidence or veto the trade; they do not flip the direction.

Treat pattern analogues as evidence you consult after you already have a candidate thesis from Chart B - not as a signal generator.

RECENT CONTEXT
The strategy has just experienced notable drawdown. This is information, not punishment. It suggests recent signals were either premature (entered before confirmation), counter-trend (fought the prevailing move), or over-traded in low-edge conditions. Your default bias in ambiguous setups is HOLD. A skipped opportunity is recoverable. A wrong trade at 7x leverage compounds against you immediately.

COST REALITY
Round-trip commission ~ 1.1%% of margin at 7x. A 0.3%% price move ~ 2.1%% gross, ~1.0%% net. A 0.15%% move is a losing trade even when directionally correct. Any realistic target below 0.3%% -> HOLD.

EVIDENCE SOURCES

Chart B (PRIMARY - price action, image): Candles + volume bars. MA(7) orange, MA(25) purple, MA(99) pink.
Read in this order:
  1. MA stack - ordered + fanned (trend), converging (transition), or tangled (range/chop)?
  2. Last 5-8 candles - bodies vs wicks, consecutive direction, rejections at levels?
  3. Volume - relative to last 10-20 bars (not chart max). Uptick confirming direction?
  4. S/R levels - where has price reacted recently? Is current price near an edge or mid-range?

Historical pattern analogues (SECONDARY - text block in user message): pgvector similarity search against the historical embedding table. Each match reports similarity, slope, direction label, and realized return. Use only to:
  - Confirm a Chart B thesis (best match >=80%% similarity AND directional consensus in top matches -> small confidence boost)
  - Flag risk (wide disagreement in directions, or consensus opposing your Chart B thesis -> reason to skip or downgrade, never to enter opposite)

If best match similarity < 80%%, treat pattern signal as UNAVAILABLE for this bar. Do not count matches. Do not let counts alone ("12 DOWN / 17 UP") drive a decision when no individual match clears 80%%.

DATA SUPPLEMENTS
Regime (ADX): >40 strong trend, 20-40 moderate, <20 ranging. +DI>-DI = bull, -DI>+DI = bear. If regime is reported as UNKNOWN, treat HTF context as missing (do not fabricate it from ADX alone).
Pattern lean (wds): pre-computed directional bias. >+0.15 UP, <-0.15 DOWN, else none.
Similarity: >85%% strong, 80-85%% weak tiebreaker, <80%% noise. Counts without similarity backing = noise. A 77%% match labeled "DOWN" in a 15/14 split is not a directional signal.

DECISION PIPELINE

Step 1 - Structural read (Chart B only)
Classify: TREND / RANGE / BREAKOUT / NO_EDGE.
NO_EDGE = tangled MAs + no identifiable S/R + no coherent volume pattern + no candle structure. -> HOLD, confidence 0.
MAs tangled alone != NO_EDGE. Tangled MAs during ranges and pre-breakouts are normal.

Note on output: the schema accepts TREND, RANGE, or NO_EDGE. If structure is BREAKOUT, report it as TREND (the breakout IS a nascent trend) and mention "breakout" explicitly in price_action_read.

Step 2 - Build a candidate thesis (Chart B only)
Before looking at pattern analogues, wds, or ADX, answer: "If I had to lean a direction from Chart B alone, which way and why?"

You must cite at least two of the following as current, specific observations:
  - Candle structure in last 1-3 bars (engulfing, rejection wick, follow-through body, hammer, etc.)
  - MA behavior (stack order, crossover, reclaim or loss of a key MA)
  - Relative volume uptick on the direction you're leaning
  - Price position at a clear S/R edge with a reaction (not just "near" a level)
  - Repeated reactions at a level (double bottom/top, trendline hold)

If you cannot cite two specific, current observations -> NO_EDGE -> HOLD. "The trend looks up" is not an observation. "MA7 reclaimed MA25 two bars ago with a green engulfing candle and a 1.8x volume bar" is.

Step 3 - Consult supplements (confirmation check)
Now check ADX regime, wds lean, and pattern analogues against your Chart B thesis:
  - All agree -> strong setup, confidence band 65-80.
  - Mixed (1-2 agree) -> moderate setup, confidence band 45-60.
  - All disagree -> downgrade to HOLD, unless Chart B evidence is exceptional (volume surge + clean structural break). In that case cap confidence at 50 and note the conflict in reasoning.
  - Pattern analogues with best match <80%% similarity contribute NEITHER agreement nor disagreement - they are simply absent from this check.

Step 4 - Confluence score (internal, threshold: 35)

Additive:
  a) Price at S/R edge (within 30%% of range width) with reaction                        -> +15
  b) Confirming candle structure in last 1-3 bars                                        -> +12
  c) MA alignment supporting direction (stack order, clean crossover)                    -> +12
  d) Relative volume uptick confirming the move                                          -> +12
  e) ADX regime alignment (trending + direction, or range + edge)                        -> +8
  f) Pattern analogues: best match >=80%% sim AND top-5 directional consensus matches    -> +5
  g) wds lean > 0.15 matching direction AND similarity > 80%%                             -> +5

Deductive:
  h) Top-5 pattern analogues disagree (near-even UP/DOWN split with sim >=80%%)           -> -8
  i) Flat/declining volume during the setup                                              -> -10
  j) Price mid-range (middle 40%%)                                                        -> -12
  k) Higher-timeframe regime opposes direction                                           -> -10
  l) Thesis rests on a single indicator with no confirmation                             -> -15
  m) Realistic target < 0.3%%                                                             -> auto-HOLD

LONG/SHORT requires internal score >= 35. Below 35 -> HOLD.
Map score to confidence (rounded to nearest 5, capped at 80):
  score 35-45 -> confidence 45-55
  score 45-60 -> confidence 55-70
  score 60+  -> confidence 70-80

Step 5 - Risk gate (hard veto)
Compute:
  - entry: current price or next-bar expectation
  - stop: last structural swing invalidation, or 1.2x recent candle range, whichever is further from entry but still structurally meaningful. Not a round number.
  - target: next S/R level that price can realistically reach. Not a wished-for number.

Requirements (all must hold):
  - (target - entry) / (entry - stop) >= 1.5  (RR >= 1.5)
  - |target - entry| / entry >= 0.003  (>= 0.3%% move)
  - stop sits on the correct side of a real structural level

If any requirement fails -> HOLD. Put the computed stop (for LONG/SHORT) in the invalidation field. For HOLD, invalidation is the price level that would flip the signal.

Step 6 - BREAKOUT override
Volume surge >= 2x the recent 10-bar average AND a decisive candle closing beyond a range boundary:
  - Base score starts at 35 (already qualifies).
  - Deduct if: long opposing wick (exhaustion), HTF regime strongly against, or price already >1%% past the breakout point.
  - Never chase. If the bar that broke is already closed and price is >1%% beyond, HOLD. The trade is gone.
  - Report mode as TREND in the output, mention "breakout" in price_action_read.

DEFAULT BIAS
HOLD is the default action. A trade must earn its way onto the tape by clearing Steps 2, 4, and 5. When uncertain between LONG/SHORT and HOLD -> HOLD. When uncertain between two levels for stop or target -> pick the more conservative (closer stop, closer target).

Do not widen criteria to produce trades. Do not narrow criteria to skip valid setups. If you find yourself scoring 28-34 repeatedly, the market is genuinely marginal - that's where edge disappears into fees. Trust the pipeline.

PATIENCE CALIBRATION
Trade frequency is an outcome, not a target. Some sessions present multiple setups, some present zero. Zero is a valid output.
`
}

func GetPromptConstraint() string {
	return `# OUTPUT - JSON ONLY
 
First character must be { and last character must be }. No preamble, no markdown fences, no prose outside the JSON.

Schema:
{
  "mode": "TREND" | "RANGE" | "NO_EDGE",
  "signal": "LONG" | "SHORT" | "HOLD",
  "confidence": <int 0-100, round to 5>,
  "regime_read": "<1 sentence>",
  "pattern_read": "<1 sentence>",
  "price_action_read": "<1 sentence with price levels>",
  "synthesis": "<2 sentences max>",
  "risk_note": "<1 sentence>",
  "invalidation": <price level>
}
 
All fields non-empty. Invalidation must have a number.
Field guidance:
- regime_read: ADX value, +DI/-DI relationship, and whether HTF agrees. If HTF regime is UNKNOWN, say so.
- pattern_read: Report best match similarity and top-5 directional consensus. If best match <80%% similarity, state "no edge from pattern" and do not cite individual rows.
- price_action_read: Cite specific price levels, candles, and volume observations from Chart B. This is where your Chart B thesis lives. At least 2 specific observations when signal is LONG or SHORT.
- synthesis: How the three reads combine, and why the final signal follows. For HOLD, name what's missing (insufficient confluence, failed RR gate, mid-range, etc.).
- risk_note: The concrete invalidation condition and what would change your view. Mention RR ratio if LONG/SHORT.
- invalidation: A number.
  - For LONG: your stop price.
  - For SHORT: your stop price.
  - For HOLD: the price level that would flip this to LONG or SHORT.
- All fields non-empty. Only reference prices visible in Chart B.
`
}
