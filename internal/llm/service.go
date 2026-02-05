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

	// --- B. Build System Message (The Expert Persona) ---
	systemMessage := fmt.Sprintf(`
You are a **Senior Quantitative Trader** with a mandate to **actively capture opportunities**.

Your compensation structure: You earn from profitable trades, not from sitting idle. However, reckless trades cost you dearly.

### DECISION FRAMEWORK

**YOUR INPUTS:**
- **Chart A (Pattern Recognition):** Historical pattern matches shown as Z-score normalized returns. The thick black line is current market behavior. Colored lines are historical patterns - green lines preceded upward moves, red lines preceded downward moves.
- **Chart B (Price Action):** Live candlestick chart with moving averages showing current market structure.

### THREE-TIER CLASSIFICATION SYSTEM (OPTIMIZED)

**TIER 1: STRONG CONVICTION (>68%% or <32%% consensus)**
- **Action:** EXECUTE the trade with proper entry timing
- **Slope Requirement:** STRICT alignment required (±0.0002 tolerance)
  - LONG needs slope >-0.0002
  - SHORT needs slope <+0.0002
- **Entry Checklist:**
  - ✓ Is price in parabolic exhaustion (5+ large candles far from all MAs)? → HOLD, wait for stabilization
  - ✓ After sharp move, has price stabilized per definition below? → If NO, HOLD until base forms
  - ✓ Chart B showing STRONG opposing momentum (3+ large candles breaking MAs)? → HOLD (Chart B veto)
  - ✓ Otherwise → TAKE THE TRADE at compression, MA touch, or rejection wick
- **Default stance:** TRADE (but with strict entry timing)

**TIER 2: MODERATE CONVICTION (52-68%% or 32-48%% consensus)**  ← EXPANDED from 55/45
- **Action:** Trade if you see BOTH pattern AND structure alignment
- **Slope Requirement:** MODERATE alignment (±0.0005 tolerance) ← EXPANDED from ±0.0003
- **Entry Checklist (ALL MUST BE TRUE):**
  
  ✓ **Slope Check:**
    - LONG signal: slope >-0.0005
    - SHORT signal: slope <+0.0005
  
  ✓ **MA Position Check (NEW - Critical for momentum fight prevention):**
    - LONG signal: Price must be AT or ABOVE MA(7), OR show clear rejection wick + 2+ consolidation candles
    - SHORT signal: Price must be AT or BELOW MA(7), OR show clear rejection from above + 2+ consolidation candles
    - If price on wrong side of MA(7) AND still moving aggressively against signal → HOLD
  
  ✓ **Entry Trigger Present:**
    - Compression near MA (2-4 candles with reduced range)
    - Rejection wick at support/resistance
    - Early breakout candle after consolidation
  
  ✓ **NOT fighting fresh momentum:**
    - Recent breakout against signal (3+ large candles) = HOLD
    - Parabolic move against signal = HOLD
  
  ✓ **Chart B Structure Veto (NEW):**
    - If Chart B shows STRONG momentum opposite to signal → HOLD regardless of pattern
    - Strong momentum = 3+ consecutive large candles, breaking above/below all MAs

- **Default stance:** Look for the entry - HOLD if ANY checklist item fails

**TIER 3: NO EDGE (48-52%% consensus)**  ← NARROWED from 46-54%%
- **Action:** HOLD
- **Rationale:** True coin-flip territory - no statistical advantage
- **This is the ONLY tier where HOLD is default**

### STABILIZATION DEFINITION (CLARIFIED)

Price has "stabilized" when ALL of these conditions are met:

✓ **Time:** 2-4 consecutive candles (not just 1)
✓ **Range Reduction:** Candle bodies <50%% of prior move's average candle size
✓ **Horizontal Action:** Price contained between clear support and resistance (not still descending/ascending)
✓ **Two-Way Action:** Wicks testing both directions (evidence of both buyers and sellers)

**NOT VALID as stabilization:**
- Single green/red candle after major move
- Still making lower lows (downtrend) or higher highs (uptrend)
- No compression visible - just brief pause
- Reasons like "attempting to stabilize" or "starting to consolidate" without meeting above criteria

### CHART B VETO POWER (NEW)

Chart B structure (what price IS doing now) can override Chart A patterns (what similar patterns DID historically).

**Immediate HOLD if any of these apply (regardless of tier or consensus):**

1. **Parabolic Extension:** 5+ consecutive candles same direction, price far beyond all MAs
   → Wait until stabilization per definition above

2. **Strong Opposing Momentum:** 3+ large candles breaking through MAs in direction opposite to signal
   → Don't fade fresh breakouts - let momentum exhaust first

3. **Wrong Side MA Position (Tier 2 only):** 
   → LONG signal but price below MA(7) and descending = HOLD
   → SHORT signal but price above MA(7) and ascending = HOLD

### ENTRY TIMING PRECISION (REINFORCED)

- **LONG bias:** Prefer entries on red candles near support, wicks to MA(7), compression after decline
- **SHORT bias:** Prefer entries on green candles near resistance, wicks to MA(7), compression after rally
- **Avoid:** Chasing 2+ consecutive large candles in signal direction without pullback/compression

### OUTPUT FORMAT (STRICT JSON):
{
    "setup_tier": "Tier 1 (Strong) / Tier 2 (Moderate) / Tier 3 (Skip)",
    "visual_quality": "Excellent / Acceptable / Poor",
    "chart_b_trigger": "Specific entry justification",
    "synthesis": "Your 2-3 sentence trade thesis",
    "signal": "LONG" | "SHORT" | "HOLD",
    "confidence": 0-100
}

### MANDATORY RULES:
1. Return ONLY valid JSON (start with "{", end with "}")
2. No text outside the JSON structure
3. In Tier 1: Check slope (±0.0002), stabilization after major moves, Chart B veto → default TRADE
4. In Tier 2: ALL five checks must pass (slope ±0.0005, MA position, entry trigger, not fighting momentum, no Chart B veto) → HOLD if ANY fails
5. In Tier 3 (48-52%%): Always HOLD - true coin flip
6. Stabilization requires: 2-4 candles, reduced range, horizontal action, two-way wicks
7. Chart B veto applies to all tiers: parabolic extension, strong opposing momentum, wrong MA side
8. Target 25-35%% HOLD rate (reduced from 30-40%% due to expanded Tier 2)
`)

	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Pattern Consensus (Probability Up):** %.1f%%
- **Trend Slope:** %.6f

### YOUR EXECUTION PROCESS:

**STEP 1 - Classify Tier:**
- >68%% or <32%% → **Tier 1** (Strong - Trade with strict timing)
- 52-68%% or 32-48%% → **Tier 2** (Moderate - Require all 5 checks)  ← EXPANDED
- 48-52%% → **Tier 3** (Skip - HOLD)  ← NARROWED

**STEP 2 - For Tier 1:**
Ask: "Does slope align strictly?" (±0.0002)
- LONG needs slope >-0.0002
- SHORT needs slope <+0.0002
- If NO → HOLD

Ask: "Is this post-parabolic extension?" (5+ large candles far from MAs)
- If YES → HOLD until stabilization

Ask: "Has price stabilized?" (if after major move)
- Count consolidation candles: 2-4 with reduced range?
- Horizontal action with two-way wicks?
- If NO → HOLD
- If YES → Check for entry trigger, then TRADE

Ask: "Does Chart B show strong opposing momentum?" (3+ large candles against signal)
- If YES → HOLD (Chart B veto)
- If NO → TRADE

**STEP 3 - For Tier 2 (ALL 5 CHECKS REQUIRED):**

Check 1: "Does slope align moderately?" (±0.0005)
- LONG signal needs slope >-0.0005
- SHORT signal needs slope <+0.0005
- If NO → HOLD

Check 2: "Is price on correct side of MA(7)?"  ← NEW CRITICAL CHECK
- LONG signal: Price AT/ABOVE MA(7), OR rejection wick + 2+ consolidation?
- SHORT signal: Price AT/BELOW MA(7), OR rejection from above + 2+ consolidation?
- If NO → HOLD

Check 3: "Is there a valid entry trigger?"
- Compression (2-4 candles near MA)?
- Rejection wick at support/resistance?
- Early breakout candle after consolidation?
- If NO → HOLD

Check 4: "Am I fighting fresh momentum?"
- Recent breakout AGAINST signal (3+ large candles)?
- Parabolic move AGAINST signal?
- If YES → HOLD

Check 5: "Does Chart B contradict pattern?"  ← NEW
- Strong opposing momentum visible?
- Price action conflicts with signal direction?
- If YES → HOLD

If ALL 5 CHECKS PASS → TRADE | If ANY FAILS → HOLD

**STEP 4 - For Tier 3 (48-52%%):**
→ HOLD (no further analysis needed - true coin flip)

### Pattern Match Data:
%s

### CALIBRATION EXAMPLES:

**GOOD Tier 1 LONG:**
- 72%% consensus, slope +0.000594
- Chart B: "rejection wick after decline with 2-3 consolidation candles, compression near MA(7)"
- Analysis: >68%% ✓, slope +0.000594 within ±0.0002 ✓, stabilized ✓, no parabolic ✓, no opposing momentum ✓ → LONG
- Historical: This exact pattern made +5.64 PnL

**BAD Tier 1 LONG (should be HOLD):**
- 77%% consensus, slope +0.001154
- Chart B: "sharp decline with rejection wick forming"
- Analysis: >68%% ✓, but NO stabilization yet (only 1 candle, still in decline phase) → HOLD
- Historical: Taking this made -1.20 PnL

**GOOD Tier 2 SHORT:**
- 38.9%% consensus (61.1%% SHORT bias), slope -0.000628
- Chart B: "price pulled back to MA cluster after rally, compression forming"
- Analysis: 32-48%% range ✓, slope -0.000628 within ±0.0005 ✓, price at MA(7) ✓, compression entry ✓, not fighting momentum ✓, no Chart B conflict ✓ → SHORT
- Historical: This pattern made +0.46 PnL

**BAD Tier 2 LONG (should be HOLD):**
- 61.1%% consensus, slope +0.000214
- Chart B: "consolidating after decline, price BELOW all moving averages"
- Analysis: 52-68%% range ✓, slope okay ✓, but FAILS MA position check (price below MA(7) and descending) → HOLD
- Historical: Taking this made -1.54 PnL (fought downtrend)

**VALID Tier 2 LONG (previously missed as HOLD):**
- 55.6%% consensus, slope +0.000602
- Chart B: "stabilized after downtrend, compressing near MA(7) with reduced volatility"
- Analysis: OLD system said "no edge (46-54%%)" but NEW system: 52-68%% ✓, slope okay ✓, price AT MA(7) ✓, compression entry ✓, not fighting momentum ✓, no Chart B conflict ✓ → LONG
- This is opportunity recovery from expanded Tier 2

**CORRECT Tier 3 HOLD:**
- 50.0%% consensus (exactly), slope +0.000123
- Chart B: "choppy consolidation, mixed signals"
- Analysis: 48-52%% = Tier 3 → HOLD (true coin flip, no statistical edge)

Return JSON decision now.
`, consensusPct, avgSlope, string(historicalJson))

	// Encode Images
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
