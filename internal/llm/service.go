package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"time-series-rag-agent/internal/ai"
)

// --- Configuration ---
const (
	LLM_API_URL          = "https://openrouter.ai/api/v1/chat/completions"
	MODEL_NAME           = "anthropic/claude-sonnet-4.5"
	CONFIDENCE_THRESHOLD = 65
)

// --- Structs for JSON Response ---
// This matches the "OUTPUT FORMAT" in your system prompt exactly
type TradeSignal struct {
	SetupTeir     string `json:"setup_tier"`
	VisualQuality string `json:"visual_quality"`
	ChartBTrigger string `json:"chart_b_trigger"`
	Synthesis     string `json:"synthesis"`
	Signal        string `json:"signal"`     // LONG, SHORT, HOLD
	Confidence    int    `json:"confidence"` // 0-100 or 0.0-1.0 (handled dynamically)
	Reasoning     string `json:"reasoning"`
}

// --- Service ---
type LLMService struct {
	ApiKey string
	Client *http.Client
}

func NewLLMService(apiKey string) *LLMService {
	return &LLMService{
		ApiKey: apiKey,
		Client: &http.Client{Timeout: 60 * time.Second}, // Increased timeout for analysis
	}
}

// 1. GenerateTradingPrompt mirrors your Python logic:
//   - Calculates Slope Statistics (Consensus)
//   - Injects the "Skeptical Risk Manager" System Prompt
//   - Prepares the Multimodal User Content
func (s *LLMService) GenerateTradingPrompt(
	currentTime string,
	matches []ai.PatternLabel,
	chartPathA string,
	chartPathB string,
) (string, string, string, string, error) {

	// --- A. Process Statistical Data ---
	type HistoricalDetail struct {
		Time            string `json:"time"`
		TrendSlope      string `json:"trend_slope"`
		TrendOutcome    string `json:"trend_outcome"`
		ImmediateReturn string `json:"immediate_return"`
		Distance        string `json:"distance"`         // <--- Added
		Similarity      string `json:"similarity_score"` // <--- Added
	}

	var cleanData []HistoricalDetail
	var slopes []float64

	for _, m := range matches {
		slope := m.NextSlope3
		if slope == 0 {
			slope = m.NextSlope5
		}
		slopes = append(slopes, slope)

		trendDir := "DOWN"
		if slope > 0 {
			trendDir = "UP"
		}

		// Calculate basic similarity % (1.0 - Distance)
		// Distance usually 0.0 to 1.0 (Cosine Distance)
		// If Distance is > 1.0 (Euclidean), this might need adjustment,
		// but for Cosine, (1-Dist)*100 is a good proxy.
		simScore := (1.0 - m.Distance) * 100
		if simScore < 0 {
			simScore = 0
		}

		cleanData = append(cleanData, HistoricalDetail{
			Time:            m.Time.Format("2006-01-02 15:04"),
			TrendSlope:      fmt.Sprintf("%.6f", slope),
			TrendOutcome:    trendDir,
			ImmediateReturn: fmt.Sprintf("%.4f%%", m.NextReturn*100),
			Distance:        fmt.Sprintf("%.4f", m.Distance), // <--- Populated
			Similarity:      fmt.Sprintf("%.1f%%", simScore), // <--- Populated
		})
	}

	// Calculate Consensus
	avgSlope := 0.0
	positiveTrends := 0
	for _, s := range slopes {
		avgSlope += s
		if s > 0 {
			positiveTrends++
		}
	}
	if len(slopes) > 0 {
		avgSlope /= float64(len(slopes))
	}

	consensusPct := 0.0
	if len(slopes) > 0 {
		consensusPct = (float64(positiveTrends) / float64(len(slopes))) * 100
	}

	historicalJson, _ := json.MarshalIndent(cleanData, "", "  ")

	// ============================================================
	// OPTIMIZED PROMPT v3 — REBALANCED FOR MORE TRADES
	// ============================================================
	// PHILOSOPHY: Block only PROVEN losers. Trade everything else.
	//
	// Problem: Current prompt takes ~3 trades/day, holds ~43/day.
	//   - 8 Tier 2 trades/day with strong confirming slopes are blocked
	//   - Top over-filters: stabilization (52), MA position (43), parabolic (35)
	//   - Meanwhile it STILL lets through 66.7% LONG (62.5% loss rate)
	//
	// Solution:
	//   - HARD BLOCK: 66.7% LONG, ≥77% LONG (proven negative EV)
	//   - RELAX: stabilization, MA position, parabolic filter, Tier 2 checks
	//   - EXPAND: allow some Tier 3 trades when slope + Chart B both strongly confirm
	//   - NET RESULT: ~8-12 trades/day instead of ~3
	// ============================================================

	systemMessage := fmt.Sprintf(`
You are a **Senior Quantitative Trader** analyzing Binance Futures.

### MANDATE:
1. EXECUTE HIGH-QUALITY TRADES — each losing trade (-$1.14 avg) costs 1.5x a missed win (+$0.77 avg)
2. EXPLAIN YOUR REASONING CONCISELY

---

### INPUTS:
- **Chart A (Pattern):** Z-score normalized returns. Black=current, Green=UP, Red=DOWN.
- **Chart B (Price Action):** Live candlesticks with MA(7), MA(25), MA(99).

---

### THREE-TIER SYSTEM

**TIER 1 (>68%%%% or <32%%%% consensus):** Strong edge. EXECUTE unless blocked by kill zone or trap.
**TIER 2 (53-68%%%% or 32-47%%%% consensus):** Moderate edge. Trade when critical checks pass.
**TIER 3 (47-53%%%% consensus):** Low edge. HOLD unless slope > |0.003| AND Chart B strongly confirms. Cap confidence at 45.

**KILL ZONES (ABSOLUTE BLOCK — checked FIRST, before ALL other logic):**
- 65.0-67.9%%%% LONG → HOLD always (37.5%%%% WR, -$8.70 cumulative)
- ≥75%%%% LONG → HOLD always (0%%%% WR on 4 trades confirmed)
- <5%%%% any direction → HOLD (pattern-matching failure, 31.7%%%% WR)
- >95%%%% any direction → HOLD (pattern-matching failure)

**ZONE PERFORMANCE (use standard tier rules — NO confidence bonuses):**
- 53-60%%%% LONG → 64%%%% WR (trade normally as Tier 2)
- 33.4-40%%%% SHORT → 61%%%% WR (trade normally as Tier 2)
- 72.2%%%% LONG → 71%%%% WR (trade normally as Tier 1)
- Do NOT lower entry standards for these zones. Apply all checks normally.

**CAUTION ZONE:**
- 60-67%%%% LONG → 48%%%% WR. Require positive slope AND price above MA(7). Otherwise HOLD.
- With Acceptable VQ: also require 2+ consolidation candles.

---

### MANDATORY ANALYSIS

**F1: TIER & SLOPE**
- State consensus %%, tier, slope value
- Tier 1 LONG: slope > 0.0000 (must be positive — only 50%%%% WR with negative slope)
- Tier 1 SHORT: slope < -0.0005 (at 22.2%%%% consensus) or < -0.0008 (at 27.8-33.3%%%%)
- Tier 2 LONG: slope > -0.0005
- Tier 2 SHORT: slope < +0.0005
- Tier 3: slope must exceed |0.003| to even consider trading

**F2: CHART B STRUCTURE** (describe in detail — I cannot see the chart)
- Price vs MA(7), MA(25), MA(99) with numbers
- Last 3-5 candles: sizes, colors, wicks
- Trend state and MA alignment
- Note if price is "AT" a MA (within ±1.0%%)

**F3: ENTRY TIMING**
- After 3+ large candles same direction: need **1-2 consolidation candles** showing ANY of: reduced range, two-way wicks, horizontal action, compression near MA
- Entry is valid on: compression at MA, rejection wick, doji/spinning top after move, breakout from micro-range
- "AT MA(7)" = within ±1.0%%

**V-RECOVERY PENALTY (CRITICAL):**
If the dominant chart pattern is a V-shaped recovery/bounce:
- Reduce confidence by 15
- Require 3+ consolidation candles (not 1-2)
- If confidence drops below 50 after penalty → HOLD
- V-recovery setups have only 29%%%% WR in backtesting (31 trades, -$20.46 PnL)
- This applies to BOTH V-recovery LONGs (catching bounces) AND V-recovery SHORTs (fading sharp rallies from lows)

**F4: MA POSITION**
- LONG: price within 1.0%% of MA(7) OR above it. Price slightly BELOW MA(7) is OK if: (a) last candle has green body or lower rejection wick, AND (b) slope confirms LONG
- SHORT: price within 1.0%% of MA(7) OR below it. Price slightly ABOVE MA(7) is OK if: (a) last candle has red body or upper rejection wick, AND (b) slope confirms SHORT
- Veto ONLY if: 5+ large candles AGAINST signal with NO pause (true parabolic), or price is >2%% away from MA(7) against signal direction

**F5: TRAP DETECTION**

**[TRAP A] 65.0-67.9%% LONG — ABSOLUTE BLOCK:**
- If consensus is between 65.0%% and 67.9%% AND signal would be LONG → return HOLD immediately
- Do NOT analyze further. 37.5%%%% WR, avg loss far exceeds avg win.
- NOTE: SHORT signals at 65-68%% consensus are NOT blocked — evaluate normally.

**[TRAP B] ≥75%% LONG — ABSOLUTE BLOCK:**
- If consensus ≥ 75%% AND signal would be LONG → return HOLD immediately
- 0%%%% WR on all tested trades. This is SHORT territory only.

**[TRAP C] FIRST BOUNCE LONG:**
- After 3+ large red candles: need at least 2 green candle BODIES (not just wicks)
- Single hammer alone = HOLD. But 2 green bodies = OK to trade.

**[TRAP D] V-RECOVERY SHORT:**
- After 3+ large green candles from a low: do NOT short unless 3+ red candles break below MA(7)
- Green-dominant consolidation = accumulation → HOLD for SHORT

**[TRAP E] 60-67%% LONG CAUTION:**
- Require: positive slope AND price at/above MA(7). Otherwise HOLD.
- With Acceptable VQ: also require 2+ consolidation candles.

---

### CHART B OVERRIDE

When consensus disagrees with clear Chart B direction, you may override:

**SHORT Override (proven — 100%%%% WR on 6 trades):**
When consensus is 47-65%% (neutral-to-LONG) BUT Chart B shows:
- 4+ consecutive red candles below all MAs, OR
- Price below declining MA(7) with lower lows and no basing
→ May SHORT with confidence capped at 55

**LONG Override:**
When consensus is 35-53%% (neutral-to-SHORT) BUT Chart B shows:
- 3+ consecutive green candles above rising MA(7) with MA turning up
→ May LONG with confidence capped at 55
→ Do NOT override based on V-recovery alone (29%%%% WR)

---

### DECISION RULES

**STEP 1 — KILL ZONE CHECK (instant HOLD):**
- 65.0-67.9%% LONG → HOLD (Trap A)
- ≥75%% LONG → HOLD (Trap B)
- <5%% or >95%% any direction → HOLD (pattern failure)
If any triggers, skip all other analysis. Return HOLD JSON immediately.

**STEP 2 — TIER CLASSIFICATION:**
- Tier 1: >68%% or <32%%
- Tier 2: 53-68%% or 32-47%%
- Tier 3: 47-53%%

**STEP 3 — DIRECTION-SPECIFIC CHECKS:**

For **TIER 1:**
- Slope check (LONG: positive | SHORT: < -0.0005 or < -0.0008)
- Quick trap scan (C, D only if applicable)
- V-recovery penalty if applicable
- No 5+ candle parabolic against signal
→ If passes: TRADE

For **TIER 2:**
- Check Trap E if 60-67%% LONG
- Slope within tolerance
- Price reasonably positioned (within ±1%% of MA(7) or on correct side)
- Entry trigger visible (consolidation, wick, compression)
- V-recovery penalty if applicable
→ If VQ = Excellent: 2 of 3 non-trap checks pass → TRADE
→ If VQ = Acceptable: require 2 of 3 checks PLUS at least one of: rejection wick, strong slope (|slope| > 0.0005), or price within 0.5%% of MA(7). Otherwise HOLD.

For **TIER 3:**
- Slope must exceed |0.003|
- Chart B must clearly support direction (3+ candles, correct side of MA(7))
→ If BOTH confirm: TRADE (confidence ≤ 45)
→ Otherwise: HOLD

**STEP 4 — CONFIDENCE:**
Base confidence:
- Tier 1: 75
- Tier 2: 65
- Tier 3: 45
Bonuses: Excellent VQ +5, rejection wick trigger +5
Penalties: caution zone (60-67%% LONG) -10, V-recovery pattern -15, borderline checks -5

---

### OUTPUT (STRICT JSON):
{
    "setup_tier": "Tier 1 (Strong) / Tier 2 (Moderate) / Tier 3 (Confirmed)",
    "visual_quality": "Excellent / Acceptable / Poor",
    "chart_b_trigger": "Specific entry pattern",
    "synthesis": "3-5 sentences: tier+slope, Chart B with numbers, trap checks, decision",
    "signal": "LONG" | "SHORT" | "HOLD",
    "confidence": 0-100
}

### RULES:
1. Return ONLY valid JSON (start with "{", end with "}")
2. Max ~600 tokens total output
3. Describe Chart B with exact numbers
4. Check kill zones FIRST before any analysis
5. QUALITY OVER QUANTITY: With avg win $0.77 and avg loss $1.14, each bad trade costs 1.5x a missed good trade. When uncertain, HOLD is higher EV.
6. A skipped win costs $0.77. A taken loss costs $1.14. Be selective.
`)
	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Consensus (Prob Up):** %.1f%%%%
- **Slope:** %.6f

### TASK: Evaluate this setup objectively. Check kill zones first, then assess whether evidence justifies the risk.

**STEP 1:** KILL ZONE? 65-67.9%%%% LONG, ≥75%%%% LONG, <5%%%%, or >95%%%% → instant HOLD. Otherwise continue.
**STEP 2:** Classify tier
**STEP 3:** Analyze Chart B (with numbers, note if within ±1%%%% of any MA)
**STEP 4:** Check for V-recovery pattern (if present: apply -15 confidence penalty, require 3+ consolidation candles)
**STEP 5:** Quick trap scan (C, D, E only)
**STEP 6:** Apply tier-specific decision rules (remember: Tier 2 + Acceptable VQ needs extra confirmation)
**STEP 7:** Set confidence with bonuses/penalties
**STEP 8:** Write synthesis

### IMPORTANT: Quality matters more than quantity.
A losing trade (-$1.14 avg) costs 1.5x more than a missed winning trade (+$0.77 avg).
Only trade when evidence clearly supports direction. When uncertain, HOLD.

### Pattern Data:
%s

Return JSON now.
`, consensusPct, avgSlope, string(historicalJson))
	b64A, err := encodeImage(chartPathA)
	if err != nil {
		return "", "", "", "", err
	}

	b64B, err := encodeImage(chartPathB)
	if err != nil {
		return "", "", "", "", err
	}

	return systemMessage, userContent, b64A, b64B, nil
}

// 2. GenerateSignal executes the request
func (s *LLMService) GenerateSignal(ctx context.Context, systemPrompt, userText, imgA_B64, imgB_B64 string) (*TradeSignal, error) {

	// Construct Payload matching OpenAI/OpenRouter Multimodal specs
	payload := map[string]interface{}{
		"model": MODEL_NAME,
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": userText,
					},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:image/png;base64,%s", imgA_B64),
						},
					},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": fmt.Sprintf("data:image/png;base64,%s", imgB_B64),
						},
					},
				},
			},
		},
		"response_format": map[string]string{"type": "json_object"},
		"max_tokens":      1000,
		"temperature":     0.1, // Low temp for analytical precision
	}

	jsonBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", LLM_API_URL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.ApiKey)
	// OpenRouter specific headers (optional but good practice)
	req.Header.Set("HTTP-Referer", "https://github.com/your-bot")
	req.Header.Set("X-Title", "Go-RAG-Trader")

	// Execute
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("API Error %d: %s", resp.StatusCode, string(body))
	}

	// Parse Response
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Safely extract content
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		fmt.Println("Error: ", result)
		return nil, fmt.Errorf("invalid response format from LLM")
	}
	firstChoice := choices[0].(map[string]interface{})
	message := firstChoice["message"].(map[string]interface{})
	contentStr := message["content"].(string)

	// Clean JSON (remove markdown ticks)
	contentStr = strings.ReplaceAll(contentStr, "```json", "")
	contentStr = strings.ReplaceAll(contentStr, "```", "")
	contentStr = strings.TrimSpace(contentStr)

	// Unmarshal
	var signal TradeSignal
	if err := json.Unmarshal([]byte(contentStr), &signal); err != nil {
		log.Printf("⚠️ JSON Parse Fail. Raw Content: %s", contentStr)
		return nil, err
	}

	// Filter Low Confidence (Python Logic Ported)
	if signal.Confidence < CONFIDENCE_THRESHOLD {
		log.Printf("⚠️ Low Confidence (%d%% < %d%%). Defaulting to HOLD.", signal.Confidence, CONFIDENCE_THRESHOLD)
		signal.Signal = "HOLD"
		signal.Reasoning = fmt.Sprintf("Confidence too low (%d%%)", signal.Confidence)
	}

	return &signal, nil
}

// Helper
func encodeImage(path string) (string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}
