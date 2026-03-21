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
	return `You are a quant analyst. Asset: Binance Futures ETHUSDT 15m.
Output: one JSON trade signal. Nothing else.

# INPUTS

CHART A (Pattern Projection):
  X=time steps, blue dashed line=now divider. Y=cumulative z-score.
  BLACK=current pattern. GREEN=historical UP. RED=historical DOWN.
  Dashed right of divider=projected paths.
  Extract: (1) black line slope into divider (2) green vs red fan dominance
  (3) fan spread tight=consensus, wide=uncertain (4) black aligns with dominant fan?

CHART B (Price Action):
  OHLC candles + volume. MA(7)=orange, MA(25)=purple, MA(99)=pink.
  Extract: (1) MA stack: 7>25>99=bull, 7<25<99=bear, tangled=transitional
  (2) price vs MAs (3) last 3-5 candle structure: continuation/exhaustion/reversal
  (4) volume confirming or diverging (5) price levels from Y-axis

STRUCTURED DATA:
  Regime: ADX<20=no trend, 20-40=moderate, >40=strong. +DI>-DI=bull, reverse=bear.
  ATR Ratio>1.5=elevated vol. Compare 4H vs 1D for agreement.
  Pattern: wds pre-computed [-1,+1]. Similarity >70%=strong, 55-70%=moderate, <55%=noise.
  Slope contradicting label = unreliable match. Return <0.01% = noise.

# STEP 0: MODE SELECTION (read MARKET STRUCTURE block first)

MODE A — TREND: trend_status=ALIVE AND is_ranging=NO
  Use regime + MA stack + pattern normally.

MODE B — RANGE: trend_status=EXHAUSTED OR is_ranging=YES
  IGNORE regime table (ADX/DI are lagging from old trend).
  IGNORE MA stack direction (MAs haven't caught up).
  FOCUS ON: support/resistance levels, rejection wicks at range edges, volume spikes.
  LONG near support with buy candle. SHORT near resistance with sell rejection.
  HOLD in mid-range. Breakout with volume → switch to MODE A.

MODE C — NO EDGE: conflicting or unreadable → HOLD.

# ANALYSIS CHAIN

STEP 1 REGIME: ADX level, +DI vs -DI, vol state, 4H/1D agreement.
  Skip if MODE B (write "DISCOUNTED — range market").

STEP 2 PATTERN: wds direction (>+0.15=UP, <-0.15=DOWN), top match sim%, Chart A fan.
  Mixed pattern is normal — doesn't block a trade alone.

STEP 3 PRICE ACTION: MA stack, candle structure, volume, support/resistance.
  In MODE B: this is the PRIMARY signal. Trade the range edges.

STEP 4 SYNTHESIZE:
  MODE A rules:
    Regime + PA agree → signal 65-85.
    PA clear + regime neutral → signal 55-65.
    Two pillars conflict → HOLD 35-50.
  MODE B rules:
    Price at support + buy signal → LONG 60-70.
    Price at resistance + sell signal → SHORT 60-70.
    Price mid-range → HOLD 40-50.
    Range break with volume → follow break 70-80.

  HOLD means active conflict or no setup. NOT "I'm unsure."
  Target HOLD rate ~25%. Confidence rounds to nearest 5.
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
