package llm

import (
	"fmt"
	"strings"
	"time-series-rag-agent/internal/database"
	market_trend "time-series-rag-agent/internal/market_trend"
)

func FormatUserPrompt(
	pnlData []database.PnLData,
	regime4h market_trend.RegimeResult,
	regime1d market_trend.RegimeResult,
	matches []HistoricalDetail,
) string {
	// 1. Format the PnL data into a string that can be included in the prompt
	pnlStr := "# PnL Table:\n"
	pnlStr += "Position Open Time | Net PnL | Signal Side | Regime 4H | Regime 1D | Duration (Hours)\n"
	pnlStr += "-------------------|---------|-------------|-----------|----------|----------------\n"
	for _, data := range pnlData {
		pnlStr += fmt.Sprintf("%s | %.2f | %s | %s | %s | %s\n",
			data.PositionOpenAt, data.NetPnL, data.SignalSide, data.Regime4H, data.Regime1D, data.DurationHours)
	}
	pnlStr += "-------------------|---------|-------------|-----------|----------|----------------\n"

	// 2. Create the final prompt by embedding the formatted PnL string
	basePromt := ""

	// Adding PnL table data
	prompt := basePromt + pnlStr

	// Adding PnL summary data
	pnlSummary := summarizePnLData(pnlData)
	prompt += "\n# PnL SUMMARY:\n" + pnlSummary + "\n\n"

	// Adding regime context
	regimePromt := "# REGIME CONTEXT:\n"
	regimePromt += "Interval | Regime | Direction | ADX | PlusDI | MinusDI | ATRRatio | BandWidth\n"

	if regime4h != (market_trend.RegimeResult{}) {
		regimePromt += fmt.Sprintf("%s | %s | %s | %.2f | %.2f | %.2f | %.2f | %.2f\n",
			"4H", regime4h.Regime, regime4h.Direction, regime4h.ADX, regime4h.PlusDI, regime4h.MinusDI, regime4h.ATRRatio, regime4h.BandWidth)
	}
	if regime1d != (market_trend.RegimeResult{}) {
		regimePromt += fmt.Sprintf("%s |%s | %s | %.2f | %.2f | %.2f | %.2f | %.2f\n",
			"1D", regime1d.Regime, regime1d.Direction, regime1d.ADX, regime1d.PlusDI, regime1d.MinusDI, regime1d.ATRRatio, regime1d.BandWidth)
	}

	prompt += regimePromt

	// Adding historical pattern matches
	pattternMatchesStr := FormatPatternMatches(matches)
	prompt += pattternMatchesStr + "\n"
	return prompt
}

func summarizePnLData(pnlData []database.PnLData) string {
	// This function can be used to create a concise summary of the PnL data
	// For example, we can calculate the average PnL, count of LONG/SHORT/HOLD signals, etc.
	// This is optional and can be included in the "regime_context" or "reasoning" fields of the final output.

	if len(pnlData) == 0 {
		return "No recent trading data available."
	}

	totalPnL := 0.0
	longCount := 0
	shortCount := 0
	holdCount := 0

	for _, data := range pnlData {
		totalPnL += data.NetPnL
		switch data.SignalSide {
		case "LONG":
			longCount++
		case "SHORT":
			shortCount++
		case "HOLD":
			holdCount++
		}
	}

	avgPnL := totalPnL / float64(len(pnlData))
	summary := fmt.Sprintf("Average PnL: %.2f | LONG: %d | SHORT: %d | HOLD: %d",
		avgPnL, longCount, shortCount, holdCount)

	return summary
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
	sb.WriteString("Top 3 closest matches:\n")

	for _, m := range top {
		sb.WriteString(fmt.Sprintf("%s | slope: %s | %-4s | return: %s | sim: %s\n",
			m.Time, m.TrendSlope, m.TrendOutcome, m.ImmediateReturn, m.Similarity,
		))
	}

	return sb.String()
}

func GetBasePrompt() string {
	return `You are a Senior Quantitative Trader analyzing Binance Futures (ETHUSDT 15m).
Your job is to decide LONG / SHORT / HOLD based on the evidence provided.
No rules are given — reason from data. EXPLAIN YOUR REASONING CONCISELY.

# INPUTS:
- **Chart A (Pattern):** Z-score normalized returns. Black=current, Green=UP, Red=DOWN.
- **Chart B (Price Action):** Live candlesticks with MA(7), MA(25), MA(99).
- PnL Table: Recent closed position performance (Net PnL, Signal Side, Regime context, Duration).

All result must be returned as JSON:
{
	"signal": "LONG" | "SHORT" | "HOLD",
	"confidence": 0-100,
	"regime_context": "one sentence on regime",
	"pattern_read": "one sentence on pattern match",
	"price_action": "Chart B with numbers",
	"reason": "2-3 sentences synthesis",
	"risk_note": "any concern that lowers confidence"
}
**NO ADDITIONAL TEXT ONLY VALID JSON ABOVE**
**ANY CONCERNS PLEASE PUT AT "risk_note" field**
`
}
