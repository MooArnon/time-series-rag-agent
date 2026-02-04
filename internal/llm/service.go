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

**TIER 1: STRONG CONVICTION (>70%% or <30%% consensus)**
- **Action:** EXECUTE the trade with proper entry timing
- **Entry Checklist:**
  - ✓ Is price in parabolic exhaustion (3+ large candles far from MA)? → HOLD, wait for stabilization
  - ✓ After sharp move, has price consolidated for 2+ candles? → If NO, HOLD until base forms
  - ✓ Otherwise → TAKE THE TRADE at compression, MA touch, or rejection wick
- **Default stance:** TRADE (but with proper entry timing)

**TIER 2: MODERATE CONVICTION (55-70%% or 30-45%% consensus)**
- **Action:** Trade if you see BOTH pattern AND structure alignment
- **Entry Checklist:**
  - ✓ Slope direction matches the signal (±0.0003 tolerance)
  - ✓ Entry trigger exists: compression near MA, rejection wick, or early breakout candle
  - ✓ NOT fighting fresh momentum (recent breakout against signal = HOLD)
  - ✓ NOT in middle of chaotic chop (erratic wicks in all directions)
- **Default stance:** Look for the entry - HOLD if slope conflicts or fighting momentum

**TIER 3: NO EDGE (46-54%% consensus)**
- **Action:** HOLD
- **This is the ONLY tier where HOLD is default**

### CRITICAL SAFETY RULES:

**RULE 1 - STABILIZATION REQUIREMENT:**
After any major move (3+ consecutive large candles same direction):
- MUST see 2-3 consolidation candles before entry
- Look for: compression, basing, range-bound action
- Avoid: "just bottomed", "exhausted", "capitulation", "completed move"
- This prevents catching falling knives and shorting breakouts

**RULE 2 - MOMENTUM ALIGNMENT (Tier 2 Only):**
- If slope direction conflicts with signal → HOLD
- If Chart B shows fresh breakout AGAINST signal → HOLD  
- Example: 33%% consensus (SHORT bias) but Chart B shows bullish breakout → HOLD

**RULE 3 - ENTRY TIMING PRECISION:**
- **LONG bias:** Prefer entries on red candles, wicks to MA, compression after decline
- **SHORT bias:** Prefer entries on green candles, wicks to MA, compression after rally
- **Avoid:** Chasing 2+ consecutive large candles in signal direction without pullback

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
3. In Tier 1: Check stabilization after major moves, otherwise default to TRADE
4. In Tier 2: Require slope alignment AND entry trigger - HOLD if either missing
5. In Tier 3 (46-54%%): Always HOLD
6. Target 30-40%% HOLD rate (not <30%%, not >50%%)
`)

	userContent := fmt.Sprintf(`
### MARKET SNAPSHOT
- **Pattern Consensus (Probability Up):** %.1f%%
- **Trend Slope:** %.6f

### YOUR EXECUTION PROCESS:

**STEP 1 - Classify Tier:**
- >70%% or <30%% → **Tier 1** (Strong - Trade with timing)
- 55-70%% or 30-45%% → **Tier 2** (Moderate - Require alignment)
- 46-54%% → **Tier 3** (Skip - HOLD)

**STEP 2 - For Tier 1:**
Ask: "Is this a post-move reversal attempt?"
- Count recent candles: 3+ large candles same direction? → YES
- If YES: "Has price consolidated 2+ candles?" 
  - If NO → HOLD (wait for base)
  - If YES → Check for entry trigger, then TRADE
- If NO big move: Check entry trigger exists → TRADE

**STEP 3 - For Tier 2:**
Ask THREE questions (ALL must be YES to trade):
1. "Does slope match signal direction?" (±0.0003 tolerance)
   - LONG signal needs slope >-0.0003
   - SHORT signal needs slope <+0.0003
2. "Is there an entry trigger?" (compression / MA touch / rejection wick)
3. "Am I fighting fresh momentum?" 
   - Recent breakout AGAINST signal? → HOLD
   
If ALL YES → TRADE | If ANY NO → HOLD

**STEP 4 - For Tier 3 (46-54%%):**
→ HOLD (no further analysis needed)

### Pattern Match Data:
%s

### EXAMPLES TO GUIDE DECISIONS:

**GOOD TIER 1 TRADE:**
- 72%% consensus LONG, slope +0.000797
- Chart B: "price consolidating near MA(25) after pullback from highs"
- Analysis: No parabolic move, has consolidated, entry trigger present → LONG

**BAD TIER 1 TRADE (HOLD instead):**
- 77%% consensus LONG, slope +0.001154  
- Chart B: "sharp decline with rejection wick forming"
- Analysis: Just completed major move, NO consolidation yet → HOLD until base forms

**GOOD TIER 2 TRADE:**
- 38%% consensus (62%% SHORT), slope -0.000628
- Chart B: "price pulled back to MA cluster after rally"
- Analysis: Slope matches, entry trigger present, not fighting momentum → SHORT

**BAD TIER 2 TRADE (HOLD instead):**
- 38%% consensus (62%% SHORT), slope +0.000624
- Chart B: "overextended move above all MAs"
- Analysis: Slope conflicts, fighting upward momentum → HOLD

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
