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
	LEVERAGE             = 3
)

// --- Structs for JSON Response ---
// This matches the "OUTPUT FORMAT" in your system prompt exactly
type TradeSignal struct {
	ChartAAnalysis string `json:"chart_a_analysis"`
	ChartBAnalysis string `json:"chart_b_analysis"`
	Synthesis      string `json:"synthesis"`
	Signal         string `json:"signal"`     // LONG, SHORT, HOLD
	Confidence     int    `json:"confidence"` // 0-100 or 0.0-1.0 (handled dynamically)
	Reasoning      string `json:"reasoning"`
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
	chartPathA string, // RAG Pattern (Line Chart)
	chartPathB string, // Price Action (Candles)
) (string, string, string, string, error) {

	// --- A. Process Statistical Data (The Go equivalent of your Python loop) ---
	type HistoricalDetail struct {
		Time            string `json:"time"`
		TrendSlope      string `json:"trend_slope"`
		TrendOutcome    string `json:"trend_outcome"`
		ImmediateReturn string `json:"immediate_return"`
	}

	var cleanData []HistoricalDetail
	var slopes []float64

	for _, m := range matches {
		// Prioritize Slope 3, fallback to Slope 5
		slope := m.NextSlope3
		if slope == 0 {
			slope = m.NextSlope5
		}
		slopes = append(slopes, slope)

		trendDir := "DOWN"
		if slope > 0 {
			trendDir = "UP"
		}

		cleanData = append(cleanData, HistoricalDetail{
			Time:            m.Time.Format("2006-01-02 15:04"),
			TrendSlope:      fmt.Sprintf("%.6f", slope),
			TrendOutcome:    trendDir,
			ImmediateReturn: fmt.Sprintf("%.4f%%", m.NextReturn*100),
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

	// --- B. Build System Prompt (Exact Copy) ---
	systemMessage := fmt.Sprintf(`
You are an **Objective Quantitative Analyst AI**. 
Your goal is to identify profitable trading opportunities by weighing statistical probability against price action.

INPUTS:
1. **Chart A (Macro):** RAG Pattern Analysis (Line Chart).
   Data Context: Normalized Market Vector. The numerical sequence provided is a Z-Score Normalized Log-Return Vector representing the market's "shape".
2. **Chart B (Micro):** Live Price Action (Candlesticks).
3. **Historical Match Details** The numeric data from Chart A

OUTPUT FORMAT:
Output ONLY a valid JSON string (no markdown, no preamble).
{
    "chart_a_analysis": "Does the Black Line generally follow the Colored Lines?",
    "chart_b_analysis": "Is there a strong reversal pattern? Or just standard noise?",
    "synthesis": "Weigh the evidence. Pattern vs. Price Action.",
    "signal": "LONG" | "SHORT" | "HOLD",
    "confidence": 0 to 100
}

DECISION LOGIC (The "Opportunity" Framework):
1. **Primary Driver (Chart A):** If the RAG Patterns show a clear direction (Consensus > 60%%), this is your primary signal. Trust the history.
2. **The "Veto" Check (Chart B):** Only signal HOLD if Chart B shows a **MAJOR** contradiction (e.g., a massive rejection candle or volume spike against the trend).
   - Do NOT hold just because of small wicks or low volatility.
   - If Chart B is "messy" but not explicitly contradictory, **TRUST CHART A**.
3. **Consensus Threshold:** You only need **60%%** statistical alignment to trigger a trade.
4. **Bias:** Do not be paralyzed by perfection. If the odds are in our favor (>60%%), take the trade.

The "Do Not Chase" Rule (Volatility Filter):
Look at the last 1-3 candles in Chart B.
- If you see a **Massive Vertical Move** (e.g., a giant red candle that is 3x bigger than the previous ones), **SIGNAL HOLD**.
- Logic: The move has likely already happened. Entering now is "chasing" and risky.
- We want to enter *before* the explosion or during a *pullback*, not *during* the crash.

ENVIRONMENT :
- The leverage will be set at %d
- The stoploss will be set at every trade, it's 2 percent from the market price... 
`, LEVERAGE)

	// --- C. Build User Message (The Evidence) ---
	userContent := fmt.Sprintf(`
### Market Context
- **Analysis Time:** %s
- **Historical Trend Consensus:** %d/%d matches trended UP (%.1f%%).
- **Average Future Slope:** %.6f

### Historical Match Details (Numeric data from Chart A)
%s

### Visual Task
Analyze the attached charts.
- **Chart A** gives you the PROBABILITY.
- **Chart B** gives you the TIMING.

**Instruction:** If Chart A looks good (>60%% match) and Chart B is not a disaster, **SIGNAL THE TRADE**. 
Do not signal HOLD unless you see a clear danger sign.
`, currentTime, positiveTrends, len(slopes), consensusPct, avgSlope, string(historicalJson))

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
