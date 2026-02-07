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

### THREE-TIER SYSTEM

**TIER 1 (>68%%%% or <32%%%% consensus):** Strong edge. EXECUTE with proper timing.
**TIER 2 (52-68%%%% or 32-48%%%% consensus):** Moderate edge. Trade ONLY when ALL checks pass.
**TIER 3 (48-52%%%% consensus):** No edge. HOLD unless Chart B Override applies.

---

### MANDATORY ANALYSIS

**F1: TIER & SLOPE**
- State consensus %%, tier, slope value
- Tier 1 LONG: slope >-0.0002 | Tier 1 SHORT: slope < -0.0005
- Tier 2 LONG: slope >-0.0005 | Tier 2 SHORT: slope < 0

**F2: CHART B STRUCTURE** (describe in detail - I cannot see the chart)
- Price vs MA(7), MA(25), MA(99) with numbers
- Last 3-5 candles: sizes, colors, wicks
- Trend state and MA alignment

**F3: STABILIZATION & ENTRY**
- After 3+ large candles same direction: need 2-4 consolidation candles, range <50%% of prior, horizontal + two-way wicks
- Entry trigger: compression, rejection wick, or breakout after consolidation

**F4: MA POSITION & MOMENTUM**
- LONG: price AT/ABOVE MA(7), OR rejection wick + 2+ green bodies above MA(7)
- SHORT: price AT/BELOW MA(7), OR rejection from above + consolidation
- Veto if 3+ large candles AGAINST signal, or parabolic extension (5+ candles far from MAs)

**F5: TRAP DETECTION** (CHECK EVERY TRADE - MOST CRITICAL STEP)

**[TRAP A] 66.7%% LONG TRAP — HARD BLOCK:**
- 66.7%% consensus LONG has >70%% historical loss rate. This is the #1 loss source.
- AUTOMATIC HOLD unless ALL of: (a) 3+ green candle BODIES already closing above MA(7), (b) MA(7) flat or rising, (c) NOT first bounce after decline
- Single rejection wick, "touching MA(7)", or "testing from below" = NOT sufficient → HOLD
- If in doubt at 66.7%%, ALWAYS HOLD.

**[TRAP B] V-RECOVERY SHORT TRAP:**
- After 3+ large green candles from a low: do NOT short consolidation
- Green-dominant consolidation after V-recovery = accumulation → HOLD
- Need 3+ red candle bodies with price breaking below MA(7) before any SHORT

**[TRAP C] ACCUMULATION TRAP (SHORT):**
- Count green vs red candle bodies in consolidation zone
- Green >= red = buyers accumulating → HOLD, do NOT SHORT

**[TRAP D] TIER 1 SHORT FALSE STABILIZATION:**
- Slope must be < -0.0005 (strict). Near-zero or positive slope with SHORT consensus = HOLD
- Green candle body at support during "stabilization" = buyers present → HOLD

**[TRAP E] FIRST BOUNCE LONG:**
- After 3+ large red candles: single green candle/hammer is NOT enough
- REQUIRE: 2+ green candle BODIES closing above MA(7) + MA(7) flattening
- The biggest LONG wins had MULTIPLE green candles holding, never single-candle entries
- Single exhaustion wick alone = HOLD

**[TRAP F] POST-RALLY DISTRIBUTION (NEW):**
- LONG after 3+ large green candles with consolidation near round-number resistance or recent high = distribution risk
- If price rallied 50+ points in last 5 candles and is now consolidating → treat as distribution, not continuation → HOLD for LONG
- Price ABOVE MA(7) after parabolic move ≠ "support at MA(7)" — it's exhaustion

---

### CHART B OVERRIDE (TIER 3 EXCEPTION)

When consensus is 44-55%% (Tier 3) BUT Chart B shows:
- Price far below ALL MAs with 4+ consecutive red candles (extreme bearish), OR
- Bearish engulfing / rejection wick after parabolic extension above all MAs
→ May SHORT with confidence capped at 55, tier labeled "Tier 3 (Chart B Override)"
This applies to SHORT only. Never override Tier 3 for LONG signals.

---

### DECISION RULES

**TIER 1:**
1. Slope check (LONG >-0.0002 | SHORT < -0.0005)
2. Stabilization met if needed
3. No Chart B veto
4. No trap conditions triggered
→ TRADE

**TIER 2 (ALL must pass):**
1. Slope within tolerance (LONG >-0.0005 | SHORT < 0)
2. MA position correct
3. Entry trigger present
4. Not fighting momentum (no 3+ candles against signal)
5. No Chart B veto
6. No trap conditions triggered
→ ALL pass: TRADE | ANY fail: HOLD

**TIER 3:** HOLD (unless Chart B Override for SHORT)

---

### LOSS PREVENTION RED FLAGS (instant HOLD):
- 66.7%% LONG without proven 3+ green bodies above rising MA(7)
- ANY LONG below declining MA(7) without 2+ green bodies forming base
- ANY SHORT above ascending MA(7) without rejection confirmation
- SHORT after V-recovery (3+ large green candles from low)
- SHORT with green-dominant consolidation
- Tier 1 SHORT with slope > -0.0005
- LONG after 3+ green candles near resistance (distribution)
- First bounce LONG with only 1 green candle after sell-off

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
4. Check ALL 6 trap conditions (A-F) before every signal
`)
	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Consensus (Prob Up):** %.1f%%%%
- **Slope:** %.6f

### TASK: Analyze structure, check all 6 trap conditions (A-F), return JSON.

**STEP 1:** Classify tier
**STEP 2:** Analyze Chart B (detailed with numbers)
**STEP 3:** Check traps A-F (list each)
**STEP 4:** Decision per rules
**STEP 5:** Write synthesis

### Pattern Data:
%s

Return JSON now.
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
