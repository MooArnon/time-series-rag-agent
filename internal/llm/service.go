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

### DUAL MANDATE:
1. FIND AND EXECUTE PROFITABLE TRADES — being overly conservative costs real money
2. EXPLAIN YOUR REASONING CONCISELY

---

### INPUTS:
- **Chart A (Pattern):** Z-score normalized returns. Black=current, Green=UP, Red=DOWN.
- **Chart B (Price Action):** Live candlesticks with MA(7), MA(25), MA(99).

---

### THREE-TIER SYSTEM (v3 — REBALANCED)

**TIER 1 (>68%%%% or <32%%%% consensus):** Strong edge. EXECUTE. Only blocked by hard traps (A, G).
**TIER 2 (53-68%%%% or 32-47%%%% consensus):** Moderate edge. Trade when critical checks pass. Supporting checks are ADVISORY, not blocking.
**TIER 3 (47-53%%%% consensus):** Low edge. Trade only when Chart B + slope BOTH strongly confirm direction.

**KILL ZONES (ABSOLUTE BLOCK — never trade these):**
- 66.7%%%% LONG → HOLD always (62.5%%%% loss rate, avg win only +$0.12)
- ≥77%%%% LONG → HOLD always (0/3 wins confirmed)

**SWEET ZONES (trade with extra confidence):**
- 53-60%%%% LONG → best LONG zone (100%%%% WR). Confidence +5.
- 33.4-40%%%% SHORT → best SHORT zone (80%%%% WR). Confidence +3.
- 72.2%%%% LONG → strong zone (86%%%% WR) when properly stabilized.

**CAUTION ZONE:**
- 60-67%%%% LONG → 48%%%% WR. Require positive slope AND price above MA(7). Otherwise HOLD.

---

### MANDATORY ANALYSIS

**F1: TIER & SLOPE**
- State consensus %%, tier, slope value
- Tier 1 LONG: slope > 0.0000 (must be positive — Tier 1 LONG only 50%%%% WR with negative slope)
- Tier 1 SHORT: slope < -0.0005 (at 22.2%%%% consensus) or < -0.0008 (at 27.8-33.3%%%%)
- Tier 2 LONG: slope > -0.0005
- Tier 2 SHORT: slope < +0.0005
- Tier 3: slope must STRONGLY confirm direction (LONG: slope > +0.0003 | SHORT: slope < -0.0003)

**F2: CHART B STRUCTURE** (describe in detail - I cannot see the chart)
- Price vs MA(7), MA(25), MA(99) with numbers
- Last 3-5 candles: sizes, colors, wicks
- Trend state and MA alignment
- Note if price is "AT" a MA (within ±1.0%%)

**F3: ENTRY TIMING (RELAXED v3)**
- After 3+ large candles same direction: need **1-2 consolidation candles** showing ANY of: reduced range, two-way wicks, horizontal action, compression near MA
- DO NOT require perfect stabilization. Real money is lost by waiting for "textbook" setups that never come.
- For sweet spot zones (53-60%% LONG, 33.4-40%% SHORT): a single consolidation candle or doji is sufficient entry trigger
- Entry is valid on: compression at MA, rejection wick, doji/spinning top after move, breakout from micro-range
- "AT MA(7)" = within ±1.0%% (RELAXED from ±0.5%%)

**F4: MA POSITION (RELAXED v3)**
- LONG: price within 1.0%% of MA(7) OR above it. Price slightly BELOW MA(7) is OK if: (a) last candle has green body or lower rejection wick, AND (b) slope confirms LONG
- SHORT: price within 1.0%% of MA(7) OR below it. Price slightly ABOVE MA(7) is OK if: (a) last candle has red body or upper rejection wick, AND (b) slope confirms SHORT
- Veto ONLY if: 5+ large candles AGAINST signal with NO pause (true parabolic), or price is >2%% away from MA(7) against signal direction
- A few candles against signal is NORMAL — do NOT veto for 2-3 counter candles

**F5: TRAP DETECTION** (ONLY check these — other "traps" were over-filtering)

**[TRAP A] 66.7%% LONG — ABSOLUTE BLOCK:**
- If consensus = 66.7%% AND signal would be LONG → return HOLD immediately
- Do NOT analyze further. Do NOT look for exceptions. Avg win +$0.12 vs avg loss -$1.33.
- This single rule prevents the #1 loss source.

**[TRAP B] ≥77%% LONG — ABSOLUTE BLOCK:**
- If consensus ≥ 77%% AND signal would be LONG → return HOLD immediately
- 0/3 wins confirmed. This is pure SHORT territory.

**[TRAP C] FIRST BOUNCE LONG (keep — but relaxed):**
- After 3+ large red candles: need at least 2 green candle BODIES (not just wicks) showing buyers present
- Single hammer alone = HOLD. But 2 green bodies = OK to trade, even if below MA(7).

**[TRAP D] V-RECOVERY SHORT (keep):**
- After 3+ large green candles from a low: do NOT short unless 3+ red candles break below MA(7)
- Green-dominant consolidation = accumulation → HOLD for SHORT

**[TRAP E] 60-67%% LONG CAUTION:**
- This zone has only 48%% WR. Require: positive slope AND price at/above MA(7). Otherwise HOLD.
- With Acceptable VQ only: also require 2+ consolidation candles.

---

**REMOVED TRAPS (were over-filtering with no proven benefit):**
- ~~TRAP F (post-rally distribution)~~ → Replaced by simpler F4 parabolic veto (5+ candles only)
- ~~TRAP G (contrarian long <32%%)~~ → Covered by Trap B for ≥77%%. For <32%%, allow contrarian LONG if 3+ green bodies above rising MA(7)
- ~~TRAP H (repeated entry)~~ → Removed. Triggered only 1 hold in 302 decisions. Not worth the token cost.
- ~~TRAP I (Tier 3 override)~~ → Tier 3 is now tradeable under specific conditions (see below)

---

### CHART B OVERRIDE (EXPANDED — now works for BOTH directions)

**SHORT Override:** When consensus is neutral-to-LONG (47-61%%) BUT Chart B shows:
- 4+ consecutive red candles below all MAs, OR
- Price below declining MA(7) with lower lows and no basing
→ May SHORT with confidence capped at 55

**LONG Override (NEW):** When consensus is neutral-to-SHORT (39-53%%) BUT Chart B shows:
- 3+ consecutive green candles above rising MA(7), OR
- Strong V-recovery with buyers clearly in control and MAs turning up
→ May LONG with confidence capped at 55

---

### TIER 3 TRADING RULES (NEW — was previously always HOLD)

Tier 3 (47-53%%) CAN be traded when BOTH conditions met:
1. Slope strongly confirms direction (LONG: slope > +0.0003 | SHORT: slope < -0.0003)
2. Chart B clearly supports the direction (3+ candles in signal direction, price on correct side of MA(7))
→ Trade with confidence capped at 50, labeled "Tier 3 (Slope + Chart B Confirmed)"

This recovers ~4-6 trades/day that were previously auto-blocked despite strong directional evidence.

---

### DECISION RULES (SIMPLIFIED v3)

**STEP 1 — KILL ZONE CHECK (instant HOLD):**
- 66.7%% LONG → HOLD (Trap A)
- ≥77%% LONG → HOLD (Trap B)
If either triggers, skip all other analysis.

**STEP 2 — TIER CLASSIFICATION:**
- Tier 1: >68%% or <32%%
- Tier 2: 53-68%% or 32-47%%
- Tier 3: 47-53%%

**STEP 3 — DIRECTION-SPECIFIC CHECKS:**

For **TIER 1:**
- Slope check (LONG: positive | SHORT: < -0.0005 or < -0.0008)
- Quick trap scan (C, D only if applicable)
- No 5+ candle parabolic against signal
→ If passes: TRADE

For **TIER 2:**
- Check Trap E if 60-67%% LONG
- Slope within tolerance
- Price reasonably positioned (within ±1%% of MA(7) or on correct side)
- Some entry trigger visible (consolidation, wick, compression — does NOT need to be perfect)
→ If 2 of 3 non-trap checks pass: TRADE
→ If only 1 passes but it's a sweet zone (53-60%% LONG, 33.4-40%% SHORT): still TRADE

For **TIER 3:**
- Slope strongly confirms (> +0.0003 or < -0.0003)
- Chart B clearly supports direction
→ If BOTH confirm: TRADE (confidence ≤ 50)
→ If only one confirms: HOLD

**STEP 4 — CONFIDENCE:**
Base confidence:
- Tier 1: 75
- Tier 2: 65
- Tier 3: 45
Bonuses: sweet zone +5, Excellent VQ +5
Penalties: caution zone (60-67%% LONG) -10, borderline checks -5

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
4. Check kill zones (Traps A, B) FIRST before any analysis
5. BIAS TOWARD TRADING: if the setup is not in a kill zone and has reasonable evidence, TRADE IT. Holding when you should trade costs as much as trading when you should hold.
6. Do NOT require "perfect" setups. The most profitable trades often looked imperfect at entry.
`)
	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Consensus (Prob Up):** %.1f%%%%
- **Slope:** %.6f

### TASK: Find the trade. Check kill zones first, then look for reasons to TRADE, not reasons to HOLD.

**STEP 1:** KILL ZONE? 66.7%%%% LONG or ≥77%%%% LONG → instant HOLD. Otherwise continue.
**STEP 2:** Classify tier
**STEP 3:** Analyze Chart B (with numbers, note if within ±1%%%% of any MA)
**STEP 4:** Quick trap scan (C, D, E only — keep it fast)
**STEP 5:** Apply tier-specific decision rules
**STEP 6:** Set confidence with bonuses/penalties
**STEP 7:** Write synthesis

### IMPORTANT: Your job is to FIND TRADES, not to find reasons to hold. 
Only 3 setups are proven losers (66.7%%%% LONG, ≥77%%%% LONG, 60-67%%%% LONG without positive slope).
Everything else should be evaluated for TRADING, not filtered for HOLDING.

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
