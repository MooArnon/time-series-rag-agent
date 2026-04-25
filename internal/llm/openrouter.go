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
	"sync/atomic"
	"time"

	"time-series-rag-agent/internal/embedding"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/trade"
)

// --- Configuration ---
const (
	LLM_API_URL = "https://api.anthropic.com/v1/messages"
	MODEL_NAME  = "claude-sonnet-4-6"
)

// --- Structs for JSON Response ---
// This matches the "OUTPUT FORMAT" in your system prompt exactly

// --- Service ---
type LLMService struct {
	ApiKey         string
	Client         *http.Client
	MaxDailyTokens int
	dailyTokens    atomic.Int64
	lastResetDay   atomic.Int64 // year*1000+dayOfYear; reset counter when this changes
}

func NewLLMService(apiKey string, maxDailyTokens int) *LLMService {
	return &LLMService{
		ApiKey:         apiKey,
		Client:         &http.Client{Timeout: 60 * time.Second},
		MaxDailyTokens: maxDailyTokens,
	}
}

func (s *LLMService) resetDailyTokensIfNeeded() {
	now := time.Now().UTC()
	key := int64(now.Year())*1000 + int64(now.YearDay())
	if s.lastResetDay.Swap(key) != key {
		s.dailyTokens.Store(0)
	}
}

// 1. GenerateTradingPrompt mirrors your Python logic:
//   - Calculates Slope Statistics (Consensus)
//   - Injects the "Skeptical Risk Manager" System Prompt
//   - Prepares the Multimodal User Content
func (s *LLMService) GenerateTradingPrompt(
	currentTime string,
	matches []embedding.PatternLabel,
	matches1h []embedding.PatternLabel,
	chartPathCandel string,
	pnlData []trade.PositionHistory,
	regimes map[string]exchange.IntervalRegime,
	dailyPnL float64,
) (string, string, string, error) {

	var cleanData []HistoricalDetail
	var cleanData1H []HistoricalDetail
	var slopes []float64
	var slopes1H []float64

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

	for _, m := range matches1h {
		slope := m.NextSlope3
		if slope == 0 {
			slope = m.NextSlope5
		}
		slopes1H = append(slopes1H, slope)

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

		cleanData1H = append(cleanData1H, HistoricalDetail{
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

	// historicalJson, _ := json.MarshalIndent(cleanData, "", "  ")

	systemMessage := GetBasePrompt()
	systemMessage += GetPromptConstraint()

	regime4h := regimes["4h"].Result
	regime1d := regimes["1d"].Result
	userContent := FormatUserPrompt(pnlData, regime4h, regime1d, cleanData, cleanData1H, dailyPnL)

	b64Canle, err := encodeImage(chartPathCandel)
	if err != nil {
		return "", "", "", err
	}

	return systemMessage, userContent, b64Canle, nil
}

// 2. GenerateSignal executes the request
func (s *LLMService) GenerateSignal(ctx context.Context, systemPrompt, userText, imgB_B64 string) (*TradeSignal, error) {
	s.resetDailyTokensIfNeeded()
	if s.MaxDailyTokens > 0 && s.dailyTokens.Load() >= int64(s.MaxDailyTokens) {
		return nil, fmt.Errorf("daily token budget exhausted (%d tokens used)", s.dailyTokens.Load())
	}

	// Construct Payload matching Anthropic Messages API spec
	payload := map[string]interface{}{
		"model":      MODEL_NAME,
		"max_tokens": 1000,
		"system": []map[string]interface{}{
			{
				"type": "text",
				"text": systemPrompt,
				"cache_control": map[string]string{
					"type": "ephemeral",
					"ttl":  "1h",
				},
			},
		},
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": userText,
					},
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": "image/png",
							"data":       imgB_B64,
						},
					},
				},
			},
		},
		"temperature": 0.1,
	}

	jsonBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.ApiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

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

	// Accumulate token usage for daily budget tracking
	if usage, ok := result["usage"].(map[string]interface{}); ok {
		in, _ := usage["input_tokens"].(float64)
		out, _ := usage["output_tokens"].(float64)
		used := int64(in + out)
		total := s.dailyTokens.Add(used)
		log.Printf("[LLMService] tokens this call: %d | daily total: %d", used, total)
	}

	// Safely extract content (Anthropic format: content[0].text)
	contentBlocks, ok := result["content"].([]interface{})
	if !ok || len(contentBlocks) == 0 {
		return nil, fmt.Errorf("invalid response format from LLM")
	}
	firstBlock := contentBlocks[0].(map[string]interface{})
	contentStr, ok := firstBlock["text"].(string)
	if !ok {
		return nil, fmt.Errorf("unexpected content block type: %v", firstBlock["type"])
	}

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
