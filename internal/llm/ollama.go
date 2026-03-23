package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

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
			Timeout: 30 * time.Second,
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

// Candidate is a simplified partner representation passed to the LLM.
// We decouple from the partner package to avoid circular imports.
type Candidate struct {
	Name       string
	Category   string
	Tags       []string
	DistanceKm float64
}

// RerankResult is the LLM's intelligent selection from candidates.
type RerankResult struct {
	Title      string      `json:"title"`
	Selections []Selection `json:"selections"`
	Reasoning  string      `json:"reasoning"`
}

type Selection struct {
	PartnerName string `json:"partner_name"`
	Role        string `json:"role"`
	Reason      string `json:"reason"`
}

// RerankCandidates is the core LLM function.
//
// Architecture: Candidate Generation → Intelligent Reranking
// (Same pattern as Spotify/Netflix/Uber recommendation systems)
//
// The rules engine generates 8-10 candidates (fast, deterministic).
// The LLM picks the 3-4 that form the BEST coherent experience bundle.
//
// What the LLM does that code CANNOT:
//   - Vibe matching: casual activities pair with casual food, not fine dining
//   - Flow reasoning: active first → food after makes sense; reverse doesn't
//   - Combination intelligence: bouldering + craft beer + street food > bouldering + spa + museum
//   - Contextual nuance: "fitness + food on a rainy evening" → cozy indoor gym + comfort food
func (c *OllamaClient) RerankCandidates(
	ctx context.Context,
	rc *models.RecommendationContext,
	candidates []Candidate,
) (*RerankResult, error) {

	prompt := buildRerankPrompt(rc, candidates)

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

	var result RerankResult
	if err := json.Unmarshal([]byte(ollamaResp.Response), &result); err != nil {
		return nil, fmt.Errorf("parse LLM JSON: %w", err)
	}

	return &result, nil
}

func buildRerankPrompt(rc *models.RecommendationContext, candidates []Candidate) string {
	candidateList := ""
	for i, c := range candidates {
		candidateList += fmt.Sprintf("%d. %s\n   Category: %s | Tags: %v | Distance: %.1fkm\n",
			i+1, c.Name, c.Category, c.Tags, c.DistanceKm)
	}

	return fmt.Sprintf(`You are an intelligent experience reranker for a mobility platform.

TASK: From the candidate list below, select 3-4 partners that form the BEST coherent experience as a BUNDLE. The combination matters more than individual quality.

THINK ABOUT:
- Vibe matching: casual activities pair with casual food, premium with premium
- Flow: what order makes sense for the time of day? (evening = dinner last, morning = brunch last)
- Proximity: partners close to each other create a smoother experience
- Complementarity: pick partners that ENHANCE each other, not random variety
- Include parking if available — the user sent GPS coordinates, they're driving

USER CONTEXT:
- Location: %s, %s (%.4f, %.4f)
- Weather: %s, %.0f°C
- Time: %s (%s, %dh available)
- Preferences: %v

CANDIDATES (pre-filtered by location and relevance):
%s
Respond ONLY with valid JSON:
{
  "title": "Short title capturing the experience vibe (max 6 words)",
  "selections": [
    {
      "partner_name": "exact name from the list above",
      "role": "primary_activity or wind_down or refuel or logistics",
      "reason": "Why THIS partner in THIS combination for THIS context"
    }
  ],
  "reasoning": "One sentence: why does this combination work as a coherent experience?"
}`,
		rc.City, rc.Neighborhood, rc.Lat, rc.Lon,
		rc.WeatherCondition, rc.Temperature,
		rc.TimeSlot, rc.DayOfWeek, int(rc.AvailableHours),
		rc.Preferences,
		candidateList,
	)
}

// QuickHaversine for displaying distance in prompts (avoids circular import with partner pkg)
func QuickHaversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	lat1R := lat1 * math.Pi / 180
	lat2R := lat2 * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1R)*math.Cos(lat2R)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Asin(math.Sqrt(a))
}

// ComposeRecommendation kept for backward compatibility with existing tests.
func (c *OllamaClient) ComposeRecommendation(
	ctx context.Context,
	rc *models.RecommendationContext,
	candidates []Candidate,
) (*RerankResult, error) {
	return c.RerankCandidates(ctx, rc, candidates)
}
