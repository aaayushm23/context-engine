package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/pkg/models"
)

type OllamaClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // overall HTTP timeout (context timeout will cancel sooner)
		},
	}
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format,omitempty"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

// ComposeRecommendation asks the LLM to compose a bundled experience
func (c *OllamaClient) ComposeRecommendation(
	ctx context.Context,
	rc *models.RecommendationContext,
	partners []partner.Partner,
) (*LLMResult, error) {

	prompt := buildPrompt(rc, partners)

	reqBody := ollamaRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
		Format: "json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/generate", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Parse the LLM's JSON response
	var result LLMResult
	if err := json.Unmarshal([]byte(ollamaResp.Response), &result); err != nil {
		return nil, fmt.Errorf("parse LLM JSON: %w", err)
	}

	return &result, nil
}

// LLMResult is the structured output we expect from the LLM
type LLMResult struct {
	Title       string          `json:"title"`
	Experiences []LLMExperience `json:"experiences"`
}

type LLMExperience struct {
	PartnerName string `json:"partner_name"`
	Category    string `json:"category"`
	Reason      string `json:"reason"`
}

func buildPrompt(rc *models.RecommendationContext, partners []partner.Partner) string {
	partnerList := ""
	for _, p := range partners {
		partnerList += fmt.Sprintf("- %s (category: %s, tags: %v)\n", p.Name, p.Category, p.SemanticTags)
	}

	return fmt.Sprintf(`You are a recommendation engine for a mobility platform.
Given a user's context and available partners, compose a bundled experience.

USER CONTEXT:
- Location: %s, %s (%.4f, %.4f)
- Weather: %s, %.0f°C
- Time: %s (%s, %dh)
- Available hours: %.0f
- Preferences: %v

AVAILABLE PARTNERS:
%s

RULES:
1. Select 2-4 partners that form a coherent experience
2. Include a parking option if available
3. Consider weather (indoor activities for rain)
4. Order activities logically (active → food → relaxation)
5. Each experience needs a reason explaining WHY it fits this context

Respond ONLY with valid JSON in this exact format:
{
  "title": "A short catchy title for the experience",
  "experiences": [
    {"partner_name": "exact name from list", "category": "the category", "reason": "why this fits"}
  ]
}`,
		rc.City, rc.Neighborhood, rc.Lat, rc.Lon,
		rc.WeatherCondition, rc.Temperature,
		rc.TimeSlot, rc.DayOfWeek, rc.Hour,
		rc.AvailableHours,
		rc.Preferences,
		partnerList,
	)
}
