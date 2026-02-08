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
	// OPTIMIZED PROMPT — Based on 110-trade + 302-HOLD analysis
	// ============================================================
	// CHANGELOG (vs previous version):
	//   1. HARD BLOCK 66.7% LONG expanded to ABSOLUTE BLOCK (62.5% loss rate confirmed)
	//   2. HARD BLOCK 77.8%+ LONG added (0/3 wins confirmed)
	//   3. NEW: 60-67% LONG WARNING ZONE (48% WR, worse than coin flip)
	//   4. Tier 1 LONG slope tightened: must be POSITIVE (was >-0.0002)
	//   5. Tier 1 SHORT slope tightened for 27.8-33.3%: <-0.0008 (was <-0.0005)
	//   6. NEW: 53-60% LONG confidence boost (100% WR, 7/7)
	//   7. NEW: Tier 2 + Excellent VQ confidence boost (78.6% WR)
	//   8. Trap F strengthened: 3+ green candles in last 8 candles (was 5)
	//   9. Traps H+I compressed for token savings
	//  10. Loss Prevention deduplicated (items already covered by traps removed)
	//  11. Chart B Override sections merged
	//  12. Stabilization relaxed for proven consensus sweet spots
	//  13. ~80-100 tokens saved total
	// ============================================================

	systemMessage := fmt.Sprintf(`
You are a **Senior Quantitative Trader** analyzing Binance Futures.

### DUAL MANDATE:
1. MAKE PROFITABLE TRADES
2. EXPLAIN YOUR REASONING

---

### INPUTS:
- **Chart A (Pattern):** Z-score normalized returns. Black=current, Green=UP, Red=DOWN.
- **Chart B (Price Action):** Live candlesticks with MA(7), MA(25), MA(99).

---

### THREE-TIER SYSTEM (OPTIMIZED v2)

**TIER 1 (>68%%%% or <32%%%% consensus):** Strong edge. EXECUTE with proper timing and slope check.
**TIER 2 (53-68%%%% or 32-47%%%% consensus):** Moderate edge. Trade when ALL critical checks pass + MAJORITY supporting checks align.
**TIER 3 (47-53%%%% consensus):** Insufficient edge. HOLD unless Chart B Override applies.

**CONSENSUS ZONES (DATA-DRIVEN):**
- 53-60%%%% LONG = BEST LONG zone (100%%%% WR, 7/7). Apply full Tier 2 discipline; boost confidence +5 when all checks pass.
- 33.4-40%%%% SHORT = BEST SHORT zone (80%%%% WR, 16/20). Enhanced Tier 2; boost confidence +3 when checks pass.
- 60-67%%%% LONG = DANGER ZONE (48%%%% WR, 14/29). Require ALL of: positive slope, price ABOVE MA(7), 3+ consolidation candles, AND Excellent VQ. With only Acceptable VQ → HOLD.
- 66.7%%%% LONG = ABSOLUTE BLOCK (62.5%%%% loss rate). See Trap A.
- ≥77%%%% LONG = ABSOLUTE BLOCK (0%%%% WR, 0/3). See Trap G.

---

### MANDATORY ANALYSIS

**F1: TIER & SLOPE**
- State consensus %%, tier, slope value
- Tier 1 LONG: slope must be POSITIVE (>0.0000). Tier 1 LONG has only 50%%%% WR historically — do NOT relax slope.
- Tier 1 SHORT (22.2%%%% consensus): slope < -0.0005
- Tier 1 SHORT (27.8-33.3%%%% consensus): slope < -0.0008 (TIGHTENED — at -0.0005 to -0.0008, wins avg +$0.04 vs losses avg -$1.33)
- Tier 2 LONG: slope >-0.0005 | Tier 2 SHORT: slope < +0.0005

**F2: CHART B STRUCTURE** (describe in detail - I cannot see the chart)
- Price vs MA(7), MA(25), MA(99) with numbers
- Last 3-5 candles: sizes, colors, wicks
- Trend state and MA alignment
- Note if price is "AT" a MA (within ±0.5%%)

**F3: STABILIZATION & ENTRY (OPTIMIZED v2)**
- After 3+ large candles same direction: need 2-3 consolidation candles, range <60%% of prior, showing compression with two-way wicks
- For PROVEN consensus sweet spots (53-60%% LONG, 33.4-40%% SHORT) with slope confirmation: 2 consolidation candles is sufficient
- Entry trigger: compression forming, rejection wick, or breakout after stabilization
- For Tier 2: "AT MA(7)" is acceptable when within ±0.5%% and price shows compression/consolidation

**F4: MA POSITION & MOMENTUM**
- LONG: price AT/ABOVE MA(7) (AT = within +0.5%%), OR price showing rejection wick from below + 2+ green bodies attempting to reclaim
- SHORT: price AT/BELOW MA(7) (AT = within -0.5%%), OR rejection from above + consolidation below
- Veto if 3+ large candles AGAINST signal, or parabolic extension (5+ candles far from MAs)

**F5: TRAP DETECTION** (CHECK EVERY TRADE - MOST CRITICAL STEP)

**[TRAP A] 66.7%% LONG — ABSOLUTE BLOCK (TIGHTENED):**
- 66.7%% consensus LONG has 62.5%% confirmed loss rate AND negative expected value (avg win +$0.12 vs avg loss -$1.33).
- ABSOLUTE HOLD — skip ALL further checks. Return HOLD immediately.
- Do NOT evaluate Chart B structure, do NOT look for "exceptions."
- Even the trades that "won" at 66.7%% LONG averaged only +$0.12 PnL — not worth the risk.
- If consensus = 66.7%% and direction = LONG → output HOLD. Period.

**[TRAP B] V-RECOVERY SHORT TRAP:**
- After 3+ large green candles from a low: do NOT short consolidation
- Green-dominant consolidation after V-recovery = accumulation → HOLD
- Need 3+ red candle bodies with price breaking below MA(7) before any SHORT

**[TRAP C] ACCUMULATION TRAP (SHORT):**
- Count green vs red candle bodies in consolidation zone
- Green >= red = buyers accumulating → HOLD, do NOT SHORT

**[TRAP D] TIER 1 SHORT FALSE STABILIZATION:**
- For 27.8-33.3%% consensus: slope must be < -0.0008 (strict). For 22.2%%: slope < -0.0005.
- Near-zero or positive slope with SHORT consensus = HOLD
- Green candle body at support during "stabilization" = buyers present → HOLD

**[TRAP E] FIRST BOUNCE LONG:**
- After 3+ large red candles: single green candle/hammer is NOT enough
- REQUIRE: 2+ green candle BODIES closing above MA(7) + MA(7) flattening
- The biggest LONG wins had MULTIPLE green candles holding, never single-candle entries
- Single exhaustion wick alone = HOLD

**[TRAP F] POST-RALLY DISTRIBUTION (STRENGTHENED):**
- LONG after 3+ large green candles in last **8 candles** (EXPANDED from 5) with consolidation near resistance = distribution risk
- If price rallied 3+ large green candles and is within 1%% of recent high → HOLD for LONG
- Post-rally LONG has confirmed 78%% loss rate (7/9 lost)
- Price ABOVE MA(7) after parabolic move ≠ "support at MA(7)" — it's exhaustion

**[TRAP G] CONTRARIAN LONG AGAINST STRONG SHORT CONSENSUS (EXPANDED):**
- When consensus is <32%% (strong SHORT / Tier 1 SHORT zone), do NOT go LONG
- **NEW: When consensus ≥77%% (e.g. 77.8%%), do NOT go LONG. 0/3 wins confirmed. This is the strongest SHORT pattern zone — ABSOLUTE HOLD for LONG.**
- Even if Chart B shows "bottoming" or "reversal" candles, the statistical edge heavily favors SHORT
- Exception for <32%% only: ONLY if price has ALREADY completed 80%%+ of the predicted move AND shows 5+ green candles above rising MA(7)
- No exception exists for ≥77%% LONG. Always HOLD.

**[TRAP H] REPEATED ENTRY:** If same setup just stopped out within 2-3 intervals, require upgraded VQ (Acceptable→Excellent). Market conditions that stopped you out likely persist.

**[TRAP I] TIER 3 OVERRIDE:** 47-53%% consensus = HOLD unless Chart B Override (SHORT only, see below). 50/50 = true randomness; even excellent price action doesn't overcome statistical noise.

---

### CHART B OVERRIDE (SHORT ONLY — MERGED)

When pattern consensus is neutral-to-LONG (47-61%%) BUT Chart B shows extreme bearish structure:
- Price far below ALL MAs with 4+ consecutive red candles, OR
- Bearish engulfing / rejection wick after parabolic extension above all MAs, OR
- Price firmly BELOW declining MA(7) with 3+ red candles, making lower lows, NO stabilization
→ May SHORT with confidence capped at 55
→ Label: "Tier 3 (Chart B Override)" if consensus 47-53%%, or "Tier 2 (Chart B Inversion)" if 53-61%%

**IMPORTANT**: Chart B Override/Inversion applies to SHORT only. Never override for LONG signals.

---

### 60-67%% LONG WARNING ZONE (NEW — DATA-DRIVEN)

This consensus range has 48%% win rate (14/29 trades, WORSE than coin flip). This is counterintuitive but confirmed by data. The moderate LONG edge is a trap — it creates overconfidence without delivering results.

**REQUIRED for 60-67%% LONG (ALL must be true):**
1. Slope must be POSITIVE (not just > -0.0005)
2. Price must be ABOVE MA(7) (not just AT)
3. 3+ consolidation candles visible (not just 2)
4. Visual quality must be Excellent (Acceptable is insufficient)
5. NOT post-rally (Trap F must clear)

If ANY of these fail → HOLD. Do NOT treat as standard Tier 2.

---

### DECISION RULES (OPTIMIZED v2)

**TIER 1:**
1. Check Trap A first (66.7%% LONG → instant HOLD)
2. Check Trap G (≥77%% LONG → instant HOLD, <32%% contrarian LONG → instant HOLD)
3. Slope check (LONG: must be POSITIVE | SHORT at 22.2%%: < -0.0005 | SHORT at 27.8-33.3%%: < -0.0008)
4. Stabilization met if needed (2-3 candles, <60%% range)
5. No Chart B veto
6. No remaining trap conditions triggered (B-F, H)
→ TRADE

**TIER 2 (CRITICAL + MAJORITY SUPPORTING):**

**CRITICAL CHECKS (ALL must pass):**
1. Consensus in Tier 2 range (32-47%% or 53-68%%)
2. No trap conditions triggered (A-I)
3. No Chart B veto (no parabolic extension, no 3+ candles fighting signal)
4. **NEW:** If 60-67%% LONG, all 5 Warning Zone conditions must pass (see above)

**SUPPORTING CHECKS (2 of 3 must pass with "good" rating):**
5. Slope within tolerance (LONG >-0.0005 | SHORT < +0.0005)
6. MA position acceptable (LONG: AT/ABOVE MA(7) within +0.5%% | SHORT: AT/BELOW MA(7) within -0.5%%)
7. Entry trigger present (compression forming, rejection wick at key level, stabilization in progress)

→ ALL critical + 2/3 supporting with good quality: TRADE
→ Critical pass but <2 supporting: HOLD
→ Any critical fail: HOLD

**CONFIDENCE BONUSES (apply after base confidence set):**
- 53-60%% LONG + all checks pass: +5 confidence
- 33.4-40%% SHORT + all checks pass: +3 confidence
- Tier 2 + Excellent VQ + all checks pass: +8 confidence (this combo has 78.6%% WR)

**TIER 3:** 
HOLD (unless Chart B Override for SHORT)

---

### LOSS PREVENTION RED FLAGS (instant HOLD):
- 66.7%% LONG (ABSOLUTE — no exceptions)
- ≥77%% LONG (ABSOLUTE — no exceptions)
- 60-67%% LONG without ALL Warning Zone conditions met
- ANY LONG below declining MA(7) without 2+ green bodies forming base
- ANY SHORT above ascending MA(7) without rejection confirmation
- SHORT after V-recovery (3+ large green candles from low)
- SHORT with green-dominant consolidation (green >= red bodies)
- Tier 1 SHORT with slope failing direction-specific threshold
- Post-rally LONG (3+ large green candles in last 8 candles near resistance)
- First bounce LONG with only 1 green candle after sell-off
- Trading Tier 3 (47-53%%) without valid Override criteria

---

### OUTPUT (STRICT JSON):
{
    "setup_tier": "Tier 1 (Strong) / Tier 2 (Moderate) / Tier 3 (Skip)",
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
4. Check ALL trap conditions (A-I) before every signal — check A and G FIRST for instant HOLD
5. For Tier 2: explicitly evaluate critical vs supporting checks
6. For 60-67%% LONG: explicitly evaluate all 5 Warning Zone conditions
`)
	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Consensus (Prob Up):** %.1f%%%%
- **Slope:** %.6f

### TASK: Analyze structure, check all trap conditions (A-I), apply optimized tier system v2.

**STEP 1:** INSTANT HOLD CHECK: Is this 66.7%%%% LONG or ≥77%%%% LONG? If yes → HOLD immediately.
**STEP 2:** Classify tier using boundaries (Tier 3 = 47-53%%%%)
**STEP 3:** If 60-67%%%% LONG, check all 5 Warning Zone conditions
**STEP 4:** Analyze Chart B (detailed with numbers, note if AT any MA)
**STEP 5:** Check traps A-I (list each — A and G first)
**STEP 6:** For Tier 2: Evaluate critical checks (must all pass) + supporting checks (need 2/3)
**STEP 7:** Consider Chart B Override/Inversion if applicable
**STEP 8:** Apply confidence bonuses if in sweet spot zones
**STEP 9:** Decision per optimized rules v2
**STEP 10:** Write synthesis

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
