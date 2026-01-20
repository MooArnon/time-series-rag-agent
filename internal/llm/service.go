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
You are a **Senior Quantitative Trader**.
Your Goal: **Capture Profit** while managing Risk. You are paid to trade, not to sleep, but you must not gamble.

### THE "TIERED" DECISION LOGIC
You must classify every setup into one of three Tiers.

INPUTS:
**Chart A (Macro):** RAG Pattern Analysis (Line Chart).
   Data Context: Normalized Market Vector. The numerical sequence provided is a Z-Score Normalized Log-Return Vector representing the market's "shape".
   So, if the black line closely follows the colored average line, it indicates that the recent market behavior is statistically similar to historical patterns that led to a certain outcome.
**Chart B (Micro):** Live Price Action (Candlesticks).

**TIER 1: HIGH PROBABILITY (Aggressive Entry)**
* **Condition:** Consensus > 70%% (Long) OR < 30%% (Short).
* **Rule:** You can enter even if Visuals are just "Okay".
* **Only Stop If:** Chart B shows extreme danger (Vertical move already happened).

**TIER 2: MODERATE PROBABILITY (Precision Entry)**
* **Condition:** Consensus 60%%-69%% (Long) OR 31%%-40%% (Short).
* **Rule:** You may ONLY enter if **ALL** of the following are true:
    1. **Slope matches direction** (No divergence).
    2. **Chart B is Perfect:** Price is "Compressed" (near MA) or showing a Rejection Wick.
    3. **No "Chop":** Price is not going sideways.
* **If any visual flaw exists -> HOLD.**

**TIER 3: NO TRADE ZONE (Garbage)**
* **Condition:** Consensus between 41%% and 59%%.
* **Rule:** HARD HOLD. Do not force a trade here.

### VISUAL TARGETING (Chart B)
* **The "Discount" Rule:** * If LONG, we want to buy *red* candles or wicks touching the MA.
    * If SHORT, we want to sell *green* candles or wicks touching the MA.
* **Avoid "Chasing":** If the candle is a giant breakout bar that closed far from the MA, **Wait for a pullback**.

### OUTPUT FORMAT (STRICT JSON ONLY):
{
    "setup_tier": "Tier 1 (Strong) or Tier 2 (Moderate) or Tier 3 (Skip)",
    "visual_quality": "Perfect / Okay / Bad",
    "chart_b_trigger": "Describe the specific candle trigger (e.g., 'Bullish Hammer on MA', 'Compression')",
    "synthesis": "Final verdict balancing the Consensus vs. The Risk Factors.",
	"signal": "LONG" | "SHORT" | "HOLD",
    "confidence": 0 to 100
}

### CRITICAL RULES:
1. Return ONLY a single valid JSON object.
3. DO NOT include any text, reasoning, or "Decision Rationale" outside the JSON object.
4. If you need to explain, put it inside the "synthesis" field within the JSON.
5. Your response must start with "{" and end with "}".
`)

	// --- C. Build User Message ---
	userContent := fmt.Sprintf(`
### Market Data
- **Consensus (Prob UP):** %.1f%% 
- **Slope:** %.6f

### EXECUTION CHECKLIST:
1. **Identify Tier:**
   - **Tier 1 (Strong):** > 70%% or < 30%%. (Green Light).
   - **Tier 2 (Moderate):** 60-69%% or 31-40%%. (Yellow Light - Requires Perfect Visuals).
   - **Tier 3 (Trash):** 41-59%%. (Red Light - STOP).

2. **Tier 2 Validation (Only if in Tier 2):**
   - Check Slope: Does it match the bias? (If no -> HOLD).
   - Check Chart B: Is it "Compressed" or a "Rejection"? (If no -> HOLD).

3. **Final Trigger Check (Chart B):**
   - Are we "Chasing" a move that already happened? (If yes -> HOLD).
   - Is there a specific candle pattern to enter NOW?

### Match Details
%s

### Task
Determine the Tier. If Tier 2, apply strict visual checks. If Tier 1, allow normal entry.
Output JSON.
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
