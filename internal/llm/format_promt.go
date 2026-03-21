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
	return `Quant analyst. Future Binance BTCUSDT 15m. Return one JSON signal.
 
# CHARTS
A (Pattern): BLACK=current, GREEN=UP history, RED=DOWN history. Dashed=projected.
  Read: black slope, green vs red fan dominance, fan spread (tight=conviction).
B (Price): Candles+volume. MA(7)orange MA(25)purple MA(99)pink.
  Read: MA stack order, last 3 candles, volume, price levels.
 
# DATA
Regime: ADX>40=strong trend, 20-40=moderate, <20=none. +DI>-DI=bull.
Pattern: wds[-1,+1] pre-computed. >+0.15=UP, <-0.15=DOWN. Sim>70%=strong.
 
# MODE (check MARKET STRUCTURE in user msg)
TREND (trend=ALIVE, ranging=NO): use regime+pattern+price action.
RANGE (trend=EXHAUSTED or ranging=YES): IGNORE regime/MAs (lagging).
  Trade range edges: LONG near support, SHORT near resistance.
  Mid-range: use wds and fan dominance to pick direction. Only HOLD if
  price is exactly mid-range AND wds is neutral AND fan is split.
 
# DECIDE
One clear pillar is enough to trade. You need CONFLICT to HOLD, not consensus to trade.
TREND: regime+PA agree→65-80. PA clear alone→55-65. Active conflict→HOLD.
RANGE: at edge→60-75. Mid-range with directional lean→55-65.
  Range break with volume→70-80.
HOLD only when you truly cannot read direction from ANY input.
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

// GetUserPromptTemplate returns the template for user messages.
// Fill in the variables from Go before sending.
func GetUserPromptTemplate() string {
	return `# CYCLE: {timestamp}

## MARKET STRUCTURE
Range (last 20 bars): {is_ranging} ({range_pct}%)
Trend: {trend_status}
Support: {support} | Resistance: {resistance}
Consecutive signals: {consec_count} {consec_side}

## REGIME
| TF | Regime | ADX | +DI | -DI | ATR | BW |
|----|--------|-----|-----|-----|-----|----|
{regime_rows}

## PATTERN ({total_matches} matches, wds: {wds})
| Sim% | Label | Slope | Return |
|------|-------|-------|--------|
{top5_rows}

High(>70%): D={high_down} U={high_up} | Med(55-70%): D={med_down} U={med_up}

## PnL
{pnl_rows}
Net: {net_pnl}

## CHARTS
Chart A (pattern): [image 1]
Chart B (price action): [image 2]

Signal.`
}
