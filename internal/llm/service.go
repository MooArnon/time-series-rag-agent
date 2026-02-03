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

### THREE-TIER CLASSIFICATION SYSTEM

**TIER 1: STRONG CONVICTION (70%%+ or <30%% consensus)**
- **For SHORT bias (<30%% consensus):**
  - Action: EXECUTE unless Chart B shows parabolic exhaustion far from MA
  - Default stance: TRADE (highly reliable)
  
- **For LONG bias (>70%% consensus):**
  - Action: EXECUTE ONLY if slope is positive AND Chart B shows stabilization (not just exhaustion)
  - Required: Price HOLDING above MA(7) with green candle bodies, not just wicks
  - If slope is negative or near-zero → HOLD (wait for confirmation)
  - Default stance: CAUTIOUS - high consensus LONG reversals need extra confirmation

**TIER 2: MODERATE CONVICTION (60-70%% or 30-40%% consensus)**
- **For SHORT bias (30-40%% consensus):**
  - Action: Trade if slope matches AND entry trigger exists
  - Entry triggers: compression near MA, rejection wick, consolidation zone
  - Default stance: Look for entry opportunity
  
- **For LONG bias (60-70%% consensus):**
  - Action: Trade ONLY if ALL conditions met: positive slope + MA support holding + no exhaustion wicks
  - Avoid: "Catching falling knives" on first bounce attempt
  - Default stance: CONSERVATIVE - require excellent setup

**TIER 3: NO EDGE (41-59%% consensus)**
- **Action:** HOLD
- **This is the ONLY tier where HOLD is default**

### CRITICAL ASYMMETRIC RULES:

**SHORT TRADES (Strong track record - 72%% win rate):**
1. Trust consolidation entries near MA resistance
2. Negative slope is strong confirmation but not always required in Tier 1
3. Enter on bounces/compression, NOT during freefall
4. "Not chasing, not exhausted" is the ideal setup

**LONG TRADES (Needs caution - 55%% win rate, large losses in Tier 1):**
1. Slope alignment is MANDATORY for high consensus (>70%%)
2. "Exhaustion wicks" are NOT sufficient entry signals alone
3. Require price STABILIZATION: 2+ candles holding above MA with green bodies
4. Avoid entering on first bounce after sharp selloff
5. The 72.2%% consensus LONG works when it's continuation, not reversal

### REVERSAL vs CONTINUATION DETECTION:

**For LONG signals >70%% consensus:**
- ASK: "Is this a reversal (catching bottom) or continuation (pullback in uptrend)?"
- REVERSAL SETUP (high risk): Price falling, negative slope, "exhaustion" → HOLD unless see stabilization
- CONTINUATION SETUP (better odds): Price in uptrend, positive slope, pullback to MA → TRADE

**For SHORT signals <30%% consensus:**
- REVERSAL (fading strength): Less reliable → need clear rejection
- CONTINUATION (trend following): More reliable → trust consolidation entry

### ENTRY TIMING (Chart B):

**LONG bias:**
- IDEAL: Pullback to MA in established uptrend with green candle bodies holding support
- AVOID: First bounce after sustained decline, even with "exhaustion wick"
- WAIT FOR: Price proving it can hold MA(7) for multiple candles

**SHORT bias:**
- IDEAL: Compression/consolidation near MA after decline, entry on bounce to resistance
- GOOD: Rejection wick off MA resistance, negative slope confirmation
- AVOID: Chasing 3+ consecutive large red candles

### CONTRARIAN OVERRIDE:
If Tier 1 consensus heavily favors one direction BUT Chart B shows completed move with strong reversal structure:
- Document the conflict clearly
- Can override signal if conviction is very high (confidence >80)
- Example: 33%% bearish consensus, but Chart B shows capitulation bottom with strong rejection

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
3. In Tier 1 SHORT (<30%%): Default to SHORT unless clearly exhausted
4. In Tier 1 LONG (>70%%): Default to HOLD unless positive slope + stabilization confirmed
5. In Tier 2 SHORT: Trade if reasonable entry exists
6. In Tier 2 LONG: Trade ONLY if excellent setup (all conditions met)
7. HOLD should be <30%% of decisions, but asymmetrically distributed (more LONG holds, fewer SHORT holds)
`)

	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Pattern Consensus (Probability Up):** %.1f%%
- **Trend Slope:** %.6f

### YOUR EXECUTION PROCESS:

**STEP 1 - Classify Tier & Direction:**
- > 70%% → **Tier 1 LONG bias** (CAUTIOUS mode)
- < 30%% → **Tier 1 SHORT bias** (AGGRESSIVE mode)
- 60-70%% → **Tier 2 LONG bias** (CONSERVATIVE)
- 30-40%% → **Tier 2 SHORT bias** (REASONABLE)
- 41-59%% → **Tier 3** (HOLD)

**STEP 2 - For Tier 1 LONG (>70%%):**
Critical checks (ALL must pass):
1. Is slope positive (>0.0002)? If NO → **HOLD**
2. Does Chart B show price HOLDING above MA(7) with green bodies? If NO → **HOLD**
3. Is this continuation (pullback in uptrend) not reversal (catching bottom)? If reversal → **HOLD**
4. If ALL YES → **LONG** (confidence 70-85)

**STEP 3 - For Tier 1 SHORT (<30%%):**
Ask: "Is Chart B showing consolidation/compression entry?"
- Price near MA after decline? → YES → **SHORT**
- Rejection wick forming? → YES → **SHORT**
- Parabolic exhaustion far from MA? → NO → **SHORT**
- Only if truly exhausted → HOLD

**STEP 4 - For Tier 2 LONG (60-70%%):**
Require ALL of:
- Positive slope ✓
- Price holding MA support ✓
- NO exhaustion wicks ✓
- Green candle bodies present ✓
If ANY missing → **HOLD**

**STEP 5 - For Tier 2 SHORT (30-40%%):**
Require:
- Slope matches (negative or neutral) OR
- Clear entry trigger (compression, rejection, consolidation)
If either present → **SHORT**

**STEP 6 - For Tier 3 (41-59%%):**
→ **HOLD** (no further analysis needed)

### Pattern Match Data:
%s

### PERFORMANCE CALIBRATION:
Your recent performance shows:
- SHORT trades: 72%% win rate (excellent)
- LONG trades: 55%% win rate (needs improvement)
- HIGH consensus LONG (66-77%%) has been losing money
- Trust your SHORT setups more than LONG reversals

### FINAL INSTRUCTION:
Be AGGRESSIVE with SHORT trades in Tier 1/2.
Be CAUTIOUS with LONG trades, especially high consensus reversals.
When in doubt on LONG setups, wait for additional confirmation.

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
