package context

import (
	"context"
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

	// Should always populate these fields
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

	// Verify TimeSlot is one of the expected values
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

func TestLocationEnricher(t *testing.T) {
	enricher := NewLocationEnricher()

	tests := []struct {
		name             string
		lat              float64
		lon              float64
		wantCity         string
		wantNeighborhood string
	}{
		{
			name: "Berlin Mitte",
			lat:  52.52, lon: 13.405,
			wantCity:         "Berlin",
			wantNeighborhood: "Mitte",
		},
		{
			name: "Berlin Kreuzberg",
			lat:  52.489, lon: 13.403,
			wantCity:         "Berlin",
			wantNeighborhood: "Kreuzberg",
		},
		{
			name: "Unknown location",
			lat:  40.7128, lon: -74.0060,
			wantCity:         "Unknown",
			wantNeighborhood: "", // will get closest, but city is Unknown
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &models.RecommendationContext{Lat: tt.lat, Lon: tt.lon}
			err := enricher.Enrich(context.Background(), rc)

			if err != nil {
				t.Fatalf("Enrich() error = %v", err)
			}
			if rc.City != tt.wantCity {
				t.Errorf("City = %s, want %s", rc.City, tt.wantCity)
			}
			if tt.wantNeighborhood != "" && rc.Neighborhood != tt.wantNeighborhood {
				t.Errorf("Neighborhood = %s, want %s", rc.Neighborhood, tt.wantNeighborhood)
			}
		})
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
