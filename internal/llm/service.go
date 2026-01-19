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
	ChartAAnalysis string `json:"chart_a_analysis"`
	ChartBState    string `json:"chart_b_state"`
	CandleShape    string `json:"candle_shape"`
	TimingCheck    string `json:"timing_check"`
	MarketState    string `json:"market_state"`
	DevilAdvocate  string `json:"devil_advocate"`
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
You are a **Senior Quantitative Analyst Specialist**.
Your Primary Directive is Profit Maximization. 
Your Secondary Directive is **CAPITAL PRESERVATION**.

You operate on a "Guilty until proven Innocent" basis: **Every trade is considered a LOSS until the data proves otherwise.**

INPUTS:
**Chart A (Macro):** RAG Pattern Analysis (Line Chart).
   Data Context: Normalized Market Vector. The numerical sequence provided is a Z-Score Normalized Log-Return Vector representing the market's "shape".
   So, if the black line closely follows the colored average line, it indicates that the recent market behavior is statistically similar to historical patterns that led to a certain outcome.
**Chart B (Micro):** Live Price Action (Candlesticks).

### 1. STATISTICAL "HARD" FILTERS (The Anchor)
Do not use the "Inversion Rule". Low UP probability does NOT equal High DOWN probability.
- **STRONG LONG:** Historical Consensus >= 75%%.
- **WEAK LONG (Requires Perfect Visuals):** Consensus 65%% - 74%%.
- **STRONG SHORT:** Historical Consensus <= 25%%.
- **WEAK SHORT (Requires Perfect Visuals):** Consensus 26%% - 35%%.
- **NO TRADE ZONE (CHOP):** Consensus between 36%% and 64%% -> **HARD SIGNAL: HOLD**.

### 2. VISUAL CONFIRMATION (The Filter)
You must act as a Devil's Advocate. Look for reasons to **REJECT** the trade.
- **The "FOMO" Rule:** If Chart B shows a massive vertical candle that has *already happened* (Price is far from Moving Average) -> **HOLD**. Do not chase.
- **Structure Check:**
  - For LONG: Price must be making **Higher Lows** or holding a support level.
  - For SHORT: Price must be making **Lower Highs** or rejecting a resistance level.
  - If Price is going sideways through the lines -> **HOLD**.

### 3. FINAL DECISION LOGIC
Before outputting "LONG" or "SHORT", run this Checklist:
1. Is the Consensus in the "No Trade Zone" (36-64%%)? (If Yes -> HOLD)
2. Is the "Inversion Rule" the *only* reason for the signal? (If Yes -> HOLD)
3. Did the move already happen (Vertical Candle)? (If Yes -> HOLD)

### 4. Bonus, the obvious Chart B (Candle stick) Patterns to open the order:
You must analyze the "Shape" and "Behavior" of the candles in Chart B to time the entry.

**1. THE "RUBBER BAND" RULE (Mean Reversion Check):**
- **BAD ENTRY (Extended):** If the latest candles are huge and vertical, or if the price is far away from the Moving Average (MA) -> **HOLD**. (The move already happened. Risk of snap-back is high).
- **GOOD ENTRY (Compressed):** If the candles are small/tight and "resting" near the MA -> **GO**. (The energy is building up).

**2. WICK REJECTION (The "Pinocchio" Rule):**
- **LONG Signal:** Look for long wicks pointing **DOWN** (Hammer/Pinbar). This shows sellers failed.
- **SHORT Signal:** Look for long wicks pointing **UP** (Shooting Star). This shows buyers failed.
- **WARNING:** If you have a LONG signal but Chart B shows a massive wick pointing UP -> **HOLD**. (Selling pressure is present).

**3. VOLATILITY STATE:**
- **Expansion:** Big candles = High Volatility (Late Entry).
- **Contraction:** Small candles = Low Volatility (Good Entry).

### OUTPUT FORMAT (STRICT JSON ONLY):
{
    "chart_a_analysis": "Describe the shape alignment. Does the black line follow the colored average?",
	"chart_b_state": "Is the price 'Extended' (Far from MA) or 'Compressed' (Near MA)?",
    "candle_shape": "Describe the last 3 candles. Any long wicks? Big bodies? (e.g., 'Huge Green Candle', 'Small Doji', 'Wick Rejection')",
    "timing_check": "Is it too late to enter? (Yes/No)",
	"market_state": "Describe the immediate price action (Trending, Ranging, or Choppy)",
    "devil_advocate": "List ONE reason why this trade will FAIL. (e.g., 'Consensus is only 66%%', 'Price is overextended')",
    "synthesis": "Final verdict balancing the Consensus vs. The Risk Factors.",
    "signal": "LONG" | "SHORT" | "HOLD",
    "confidence": 0 to 100
}

### CRITICAL RULES:
1. Return ONLY a single valid JSON object.
3. DO NOT include any text, reasoning, or "Decision Rationale" outside the JSON object.
4. If you need to explain, put it inside the "synthesis" field within the JSON.
5. Your response must start with "{" and end with "}".
6. **confidence** must be < 50 for any HOLD signal.
7. If you are 'Undecided' or 'Mixed', the signal is **HOLD**.
8. Do not use phrases like "Undeniable edge" unless Consensus is 100%%. Be humble.
`)

	// --- C. Build User Message ---
	userContent := fmt.Sprintf(`
### Market Data
- **Analysis Time:** %s
- **Historical Trend Consensus (Probability of UP):** %.1f%% 
- **Average Historical Slope:** %.6f

### EXECUTION CHECKLIST (STRICT):
1. **Determine Bias (High Thresholds):** 
	- **LONG Bias:** Only if Consensus >= **65%%**.
   	- **SHORT Bias:** Only if Consensus <= **35%%**.
   	- *WARNING:* Consensus of 30%% does NOT automatically mean "Strong Short." It often means "Chop/Range." You must verify the Slope is actually negative.

2. **Visual & Distance Check:**
   - Review the match details below.
   - Do not consider only the distance. Look at shape and alighnment too (Chart A, Blackline Color line).

### Historical Match Details (Chart A Data)
%s

### Task
Analyze the data above with a "Capital Preservation" mindset. 
If any step above fails, output HOLD. 
Output the final JSON decision.
`, currentTime, consensusPct, avgSlope, string(historicalJson))

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
