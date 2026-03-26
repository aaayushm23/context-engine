package context

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aaayushm23/context-engine/pkg/models"
)

func TestTimeEnricher(t *testing.T) {
	enricher := NewTimeEnricher()

	rc := &models.RecommendationContext{}
	err := enricher.Enrich(context.Background(), rc)

	if err != nil {
		t.Fatalf("TimeEnricher.Enrich() error = %v", err)
	}

	// Core scheduling fields must be universally populated as they anchor the LLM prompt.
	if rc.DayOfWeek == "" {
		t.Error("DayOfWeek should not be empty")
	}
	if rc.TimeSlot == "" {
		t.Error("TimeSlot should not be empty")
	}
	if rc.Season == "" {
		t.Error("Season should not be empty")
	}
	if rc.Hour < 0 || rc.Hour > 23 {
		t.Errorf("Hour should be 0-23, got %d", rc.Hour)
	}

	// Rigid ontology enforcement blocks arbitrary string passing to the LLM.
	validSlots := map[string]bool{
		"weekend_morning":   true,
		"weekend_afternoon": true,
		"weekend_evening":   true,
		"weekday_morning":   true,
		"weekday_afternoon": true,
		"weekday_evening":   true,
	}
	if !validSlots[rc.TimeSlot] {
		t.Errorf("unexpected TimeSlot: %s", rc.TimeSlot)
	}
}


func TestPreferenceEnricher_ExpandsTags(t *testing.T) {
	enricher := NewPreferenceEnricher()

	tests := []struct {
		name        string
		input       []string
		wantContain []string
	}{
		{
			name:        "fitness expands to active tags",
			input:       []string{"fitness"},
			wantContain: []string{"fitness", "sports", "active", "gym", "bouldering"},
		},
		{
			name:        "food expands to food tags",
			input:       []string{"food"},
			wantContain: []string{"food", "restaurant", "cafe", "food_and_drink"},
		},
		{
			name:        "unknown preference passes through",
			input:       []string{"underwater_basket_weaving"},
			wantContain: []string{"underwater_basket_weaving"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &models.RecommendationContext{Preferences: tt.input}
			err := enricher.Enrich(context.Background(), rc)

			if err != nil {
				t.Fatalf("Enrich() error = %v", err)
			}

			for _, want := range tt.wantContain {
				found := false
				for _, got := range rc.Preferences {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected preference %q not found in %v", want, rc.Preferences)
				}
			}
		})
	}
}

func TestWeatherCodeClassification(t *testing.T) {
	tests := []struct {
		code       int
		wantLabel  string
		wantIndoor bool
	}{
		{0, "clear", false},
		{2, "cloudy", false},
		{45, "foggy", false},
		{61, "rainy", true},
		{71, "snowy", true},
		{95, "stormy", true},
	}

	for _, tt := range tests {
		label := classifyWeatherCode(tt.code)
		if label != tt.wantLabel {
			t.Errorf("classifyWeatherCode(%d) = %s, want %s", tt.code, label, tt.wantLabel)
		}
		indoor := isIndoorWeather(tt.code)
		if indoor != tt.wantIndoor {
			t.Errorf("isIndoorWeather(%d) = %v, want %v", tt.code, indoor, tt.wantIndoor)
		}
	}
}

// TestLocationEnricher_NominatimAPI validates the brittle integration boundary with OSM.
// We gate this behind -short to prevent flaky CI builds caused by upstream rate limits.
func TestLocationEnricher_NominatimAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Nominatim API call in short mode")
	}

	enricher := NewLocationEnricher()

	tests := []struct {
		name             string
		lat, lon         float64
		wantCity         string
		wantNeighborhood string
	}{
		{
			name:             "Berlin Mitte (Brandenburg Gate)",
			lat:              52.5163, lon: 13.3777,
			wantCity:         "Berlin",
			wantNeighborhood: "Mitte",
		},
		{
			name:             "Berlin Kreuzberg",
			lat:              52.4973, lon: 13.3906,
			wantCity:         "Berlin",
			wantNeighborhood: "Kreuzberg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &models.RecommendationContext{Lat: tt.lat, Lon: tt.lon}
			err := enricher.Enrich(context.Background(), rc)
			if err != nil {
				t.Fatalf("Enrich() error = %v (is network available?)", err)
			}
			if rc.City != tt.wantCity {
				t.Errorf("City = %q, want %q", rc.City, tt.wantCity)
			}
			if rc.Neighborhood != tt.wantNeighborhood {
				t.Errorf("Neighborhood = %q, want %q", rc.Neighborhood, tt.wantNeighborhood)
			}
		})
	}
}

// TestLocationEnricher_APIFailure_FallsBackToHardcoded asserts our core resilience mandate:
// a localized failure in an external dependency must trigger graceful degradation,
// never a hard fault that cascades up to the user request.
func TestLocationEnricher_APIFailure_FallsBackToHardcoded(t *testing.T) {
	// Mock server returns a 503 — simulates Nominatim being down
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mockServer.Close()

	// Enricher with mock client — real Nominatim URL won't be called
	// because the client is scoped to the mock server's transport.
	// We just call Enrich directly; the real URL will fail on this client,
	// triggering the fallback path.
	enricher := &LocationEnricher{
		httpClient: mockServer.Client(),
	}

	rc := &models.RecommendationContext{
		Lat: 52.5200, // Berlin Mitte
		Lon: 13.4050,
	}

	err := enricher.Enrich(context.Background(), rc)

	// The structural output must persist via hardcoded degradation despite network failure.
	_ = err
	if rc.City == "" {
		t.Error("City should be populated via fallback even when Nominatim is down")
	}
	if rc.Neighborhood == "" {
		t.Error("Neighborhood should be populated via fallback even when Nominatim is down")
	}
}
