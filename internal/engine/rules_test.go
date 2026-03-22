package engine

import (
	"testing"

	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/pkg/models"
)

func TestRuleBasedFallback_ReturnsRecommendation(t *testing.T) {
	rc := &models.RecommendationContext{
		Lat:              52.52,
		Lon:              13.405,
		City:             "Berlin",
		Neighborhood:     "Mitte",
		WeatherTag:       "indoor_weather",
		WeatherCondition: "rainy",
		TimeSlot:         "weekend_afternoon",
		AvailableHours:   3,
		Preferences:      []string{"fitness", "food"},
		SemanticTags:     []string{"indoor", "rainy_day", "fitness", "food", "weekend"},
		SignalsUsed:      []models.ContextSignal{"weather", "time", "location", "preferences"},
	}

	partners := []partner.Partner{
		{ID: "1", Name: "BoulderKlub", Category: "indoor_activity", SemanticTags: []string{"indoor", "fitness", "rainy_day"}, Lat: 52.489, Lon: 13.403},
		{ID: "2", Name: "Stone Brewing", Category: "food_and_drink", SemanticTags: []string{"food", "restaurant"}, Lat: 52.483, Lon: 13.450},
		{ID: "3", Name: "Tiergarten", Category: "outdoor_activity", SemanticTags: []string{"outdoor", "nature"}, Lat: 52.514, Lon: 13.350},
		{ID: "4", Name: "ParkNow", Category: "parking", SemanticTags: []string{"parking", "central"}, Lat: 52.521, Lon: 13.413},
	}

	rec := RuleBasedFallback(rc, partners)

	// Must always return something
	if len(rec.Experiences) == 0 {
		t.Fatal("expected at least one experience")
	}

	// Source must be "rules"
	if rec.Source != "rules" {
		t.Errorf("source = %s, want rules", rec.Source)
	}

	// Should prefer indoor partners when weather is rainy
	firstExp := rec.Experiences[0]
	if firstExp.Category == "outdoor_activity" {
		t.Error("outdoor activity should NOT be top pick in rainy weather")
	}
}

func TestRuleBasedFallback_CategoryDiversity(t *testing.T) {
	rc := &models.RecommendationContext{
		Lat: 52.52, Lon: 13.405,
		SemanticTags:   []string{"indoor", "fitness", "food"},
		WeatherTag:     "indoor_weather",
		AvailableHours: 3,
		SignalsUsed:    []models.ContextSignal{"weather"},
	}

	// All same category
	partners := []partner.Partner{
		{ID: "1", Name: "Gym A", Category: "indoor_activity", SemanticTags: []string{"indoor", "fitness"}, Lat: 52.52, Lon: 13.40},
		{ID: "2", Name: "Gym B", Category: "indoor_activity", SemanticTags: []string{"indoor", "fitness"}, Lat: 52.52, Lon: 13.41},
		{ID: "3", Name: "Café", Category: "food_and_drink", SemanticTags: []string{"food"}, Lat: 52.52, Lon: 13.42},
		{ID: "4", Name: "Park", Category: "parking", SemanticTags: []string{"parking"}, Lat: 52.52, Lon: 13.43},
	}

	rec := RuleBasedFallback(rc, partners)

	// Should have diverse categories, not all indoor_activity
	categories := map[string]bool{}
	for _, exp := range rec.Experiences {
		categories[exp.Category] = true
	}

	if len(categories) < 2 {
		t.Errorf("expected category diversity, got categories: %v", categories)
	}
}

func TestRuleBasedFallback_EmptyPartners(t *testing.T) {
	rc := &models.RecommendationContext{
		SemanticTags:   []string{"fitness"},
		AvailableHours: 2,
		SignalsUsed:    []models.ContextSignal{"time"},
	}

	rec := RuleBasedFallback(rc, []partner.Partner{})

	// Should not panic, should return empty but valid recommendation
	if rec.Source != "rules" {
		t.Errorf("source = %s, want rules", rec.Source)
	}
}

func TestScorePartner_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		context      *models.RecommendationContext
		partner      partner.Partner
		wantMinScore float64
		wantMaxScore float64
	}{
		{
			name: "perfect match: indoor + rainy + close",
			context: &models.RecommendationContext{
				Lat: 52.52, Lon: 13.405,
				SemanticTags: []string{"indoor", "fitness", "rainy_day"},
				WeatherTag:   "indoor_weather",
			},
			partner: partner.Partner{
				Category:     "indoor_activity",
				SemanticTags: []string{"indoor", "fitness", "rainy_day"},
				Lat:          52.52, Lon: 13.41,
				GeoFenceRadiusKm: 5.0,
			},
			wantMinScore: 0.8,
			wantMaxScore: 1.0,
		},
		{
			name: "no tag overlap scores low",
			context: &models.RecommendationContext{
				Lat: 52.52, Lon: 13.405,
				SemanticTags: []string{"outdoor", "hiking"},
				WeatherTag:   "outdoor_weather",
			},
			partner: partner.Partner{
				Category:     "indoor_activity",
				SemanticTags: []string{"indoor", "gaming"},
				Lat:          52.52, Lon: 13.41,
				GeoFenceRadiusKm: 5.0,
			},
			wantMinScore: 0.0,
			wantMaxScore: 0.35,
		},
		{
			name: "out of geo-fence scores lower distance",
			context: &models.RecommendationContext{
				Lat: 52.52, Lon: 13.405,
				SemanticTags: []string{"food"},
				WeatherTag:   "outdoor_weather",
			},
			partner: partner.Partner{
				Category:     "food_and_drink",
				SemanticTags: []string{"food"},
				Lat:          48.13, Lon: 11.58, // Munich — way outside fence
				GeoFenceRadiusKm: 5.0,
			},
			wantMinScore: 0.0,
			wantMaxScore: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scorePartner(tt.context, tt.partner)
			if score < tt.wantMinScore || score > tt.wantMaxScore {
				t.Errorf("scorePartner() = %.2f, want between %.2f and %.2f",
					score, tt.wantMinScore, tt.wantMaxScore)
			}
		})
	}
}
