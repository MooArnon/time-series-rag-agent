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
- **Action:** EXECUTE the trade unless Chart B shows you'd be chasing an exhausted move
- **Checklist:**
  - ✓ Is the move already complete (parabolic candles far from MA)? → Wait for retest
  - ✓ Otherwise → TAKE THE TRADE
- **Default stance:** TRADE (not HOLD)

**TIER 2: MODERATE CONVICTION (60-70%% or 30-40%% consensus)**
- **Action:** Trade if you see a CLEAR entry trigger on Chart B
- **Checklist:**
  - ✓ Slope direction matches the signal
  - ✓ Entry trigger exists: compression near MA, rejection wick, or early breakout candle
  - ✓ NOT in the middle of chaotic chop (erratic wicks in all directions)
- **Default stance:** Look for the entry - HOLD only if all triggers are missing

**TIER 3: NO EDGE (41-59%% consensus)**
- **Action:** HOLD
- **This is the ONLY tier where HOLD is default**

### CRITICAL MINDSET SHIFTS:
1. **"Good enough" is tradeable:** Don't demand perfection in Tier 1. If consensus is strong and Chart B isn't terrible, take it.
2. **Slope is a GUIDE, not a VETO:** A slightly mismatched slope in Tier 1 doesn't kill the trade. Pattern strength > slope precision.
3. **Compression/MA touch is COMMON:** Don't wait for the "perfect" candle. If price is near MA in the right direction, that IS your entry.
4. **You're pattern-trading, not price-predicting:** Chart A patterns are statistically validated. Trust the system when consensus is clear.

### ENTRY TIMING (Chart B):
- **LONG bias:** Prefer entries on red candles, wicks to MA, or early consolidation break
- **SHORT bias:** Prefer entries on green candles, wicks to MA, or early consolidation break  
- **Avoid:** Chasing 3+ consecutive large candles in the signal direction with no pullback

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
3. In Tier 1: Default to LONG/SHORT unless Chart B is clearly exhausted
4. In Tier 2: Default to LONG/SHORT if ANY reasonable entry trigger exists
5. HOLD should be <30%% of all decisions (you're a trader, not a spectator)
`)

	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Pattern Consensus (Probability Up):** %.1f%%
- **Trend Slope:** %.6f

### YOUR EXECUTION PROCESS:

**STEP 1 - Classify Tier:**
- > 70%% or < 30%% → **Tier 1** (Strong - Bias toward TRADE)
- 60-70%% or 30-40%% → **Tier 2** (Moderate - Find the entry)  
- 41-59%% → **Tier 3** (Skip - HOLD)

**STEP 2 - For Tier 1:**
Ask: "Has Chart B already completed the move?" 
- If YES (parabolic, far from MA) → Wait for retest (HOLD)
- If NO → **EXECUTE THE TRADE**

**STEP 3 - For Tier 2:**
Ask: "Is there ANY valid entry on Chart B?"
- Compression? → Yes? → TRADE
- Rejection wick? → Yes? → TRADE  
- Near MA? → Yes? → TRADE
- Slope matches? Bonus, but not required if patterns are strong
- If ALL of the above are NO → HOLD

**STEP 4 - For Tier 3:**
→ HOLD (no further analysis needed)

### Pattern Match Data:
%s

### FINAL INSTRUCTION:
Be AGGRESSIVE in Tier 1. Be REASONABLE in Tier 2. Only be DEFENSIVE in Tier 3.
Your job is to trade, not to wait for perfection.

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
