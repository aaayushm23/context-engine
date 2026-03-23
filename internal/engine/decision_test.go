package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aaayushm23/context-engine/internal/llm"
	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/internal/resilience"
	"github.com/aaayushm23/context-engine/pkg/models"
)

func mockOllama(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func TestLLMFallback_WhenOllamaTimesOut(t *testing.T) {
	server := mockOllama(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
	})
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model")

	rc := &models.RecommendationContext{
		Lat: 52.52, Lon: 13.405,
		SemanticTags: []string{"indoor", "fitness"},
		WeatherTag:   "indoor_weather",
	}

	candidates := []llm.Candidate{
		{Name: "Test Gym", Category: "indoor_activity", Tags: []string{"indoor", "fitness"}, DistanceKm: 0.5},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.RerankCandidates(ctx, rc, candidates)

	if err == nil {
		t.Fatal("expected timeout error from LLM, got nil")
	}

	// Verify fallback still works
	partners := []partner.Partner{
		{ID: "1", Name: "Test Gym", Category: "indoor_activity", SemanticTags: []string{"indoor", "fitness"}, Lat: 52.52, Lon: 13.41, GeoFenceRadiusKm: 5.0},
	}
	rec := RuleBasedFallback(rc, partners)
	if rec.Source != "rules" {
		t.Errorf("fallback source = %s, want rules", rec.Source)
	}
	if len(rec.Experiences) == 0 {
		t.Error("fallback should still return experiences")
	}
}

func TestLLMFallback_WhenOllamaReturnsGarbage(t *testing.T) {
	server := mockOllama(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"response": "this is not valid JSON at all {{{",
		})
	})
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model")

	rc := &models.RecommendationContext{
		Lat: 52.52, Lon: 13.405,
		SemanticTags: []string{"food"},
	}
	candidates := []llm.Candidate{
		{Name: "Café", Category: "food_and_drink", Tags: []string{"food"}, DistanceKm: 0.5},
	}

	_, err := client.RerankCandidates(context.Background(), rc, candidates)

	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}

	partners := []partner.Partner{
		{ID: "1", Name: "Café", Category: "food_and_drink", SemanticTags: []string{"food"}, Lat: 52.52, Lon: 13.41, GeoFenceRadiusKm: 5.0},
	}
	rec := RuleBasedFallback(rc, partners)
	if len(rec.Experiences) == 0 {
		t.Error("fallback should return experiences even when LLM returns garbage")
	}
}

func TestLLMFallback_WhenOllamaIsDown(t *testing.T) {
	client := llm.NewOllamaClient("http://localhost:99999", "test-model")

	rc := &models.RecommendationContext{
		Lat: 52.52, Lon: 13.405,
		SemanticTags: []string{"fitness"},
	}
	candidates := []llm.Candidate{
		{Name: "Gym", Category: "indoor_activity", Tags: []string{"fitness"}, DistanceKm: 0.5},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.RerankCandidates(ctx, rc, candidates)

	if err == nil {
		t.Fatal("expected connection error, got nil")
	}

	partners := []partner.Partner{
		{ID: "1", Name: "Gym", Category: "indoor_activity", SemanticTags: []string{"fitness"}, Lat: 52.52, Lon: 13.41, GeoFenceRadiusKm: 5.0},
	}
	rec := RuleBasedFallback(rc, partners)
	if rec.Source != "rules" {
		t.Errorf("source = %s, want rules", rec.Source)
	}
}

func TestCircuitBreaker_ProtectsLLM(t *testing.T) {
	callCount := 0

	server := mockOllama(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(500)
	})
	defer server.Close()

	client := llm.NewOllamaClient(server.URL, "test-model")
	cb := resilience.NewCircuitBreaker("llm-test", 2, 5*time.Second)

	rc := &models.RecommendationContext{SemanticTags: []string{"food"}}
	candidates := []llm.Candidate{
		{Name: "Café", Category: "food_and_drink", Tags: []string{"food"}, DistanceKm: 0.5},
	}

	for i := 0; i < 3; i++ {
		cb.Execute(func() error {
			_, err := client.RerankCandidates(context.Background(), rc, candidates)
			return err
		})
	}

	if callCount > 2 {
		t.Errorf("server called %d times, expected max 2 (circuit should have opened)", callCount)
	}

	if cb.State() != resilience.StateOpen {
		t.Errorf("circuit state = %v, want Open", cb.State())
	}
}

func TestDecisionEngine_PartialContext(t *testing.T) {
	rc := &models.RecommendationContext{
		Lat: 52.52, Lon: 13.405,
		AvailableHours: 2,
		Preferences:    []string{"food"},
		SemanticTags:   []string{"food"},
		WeatherTag:     "",
		TimeSlot:       "weekend_afternoon",
		City:           "Berlin",
		Neighborhood:   "Mitte",
		SignalsUsed:     []models.ContextSignal{"time", "location", "preferences"},
		SignalsFailed:   []models.ContextSignal{"weather"},
	}

	partners := []partner.Partner{
		{ID: "1", Name: "Outdoor Café", Category: "food_and_drink", SemanticTags: []string{"food", "outdoor"}, Lat: 52.52, Lon: 13.41, GeoFenceRadiusKm: 5.0},
		{ID: "2", Name: "Indoor Restaurant", Category: "food_and_drink", SemanticTags: []string{"food", "indoor"}, Lat: 52.52, Lon: 13.40, GeoFenceRadiusKm: 5.0},
	}

	rec := RuleBasedFallback(rc, partners)

	if len(rec.Experiences) == 0 {
		t.Fatal("should return experiences even with partial context")
	}

	foundFailed := false
	for _, s := range rec.ContextSignalsFailed {
		if s == "weather" {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Error("should report weather as failed signal")
	}
}
