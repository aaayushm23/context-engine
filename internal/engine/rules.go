package engine

import (
	"fmt"
	"sort"

	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/pkg/models"
)

type scored struct {
	partner partner.Partner
	score   float64
}

// RuleBasedFallback generates recommendations without the LLM
// Used when: LLM times out, LLM errors, or circuit breaker is open
func RuleBasedFallback(rc *models.RecommendationContext, partners []partner.Partner) models.Recommendation {
	// Score each partner
	var scoredPartners []scored
	for _, p := range partners {
		score := scorePartner(rc, p)
		scoredPartners = append(scoredPartners, scored{partner: p, score: score})
	}

	// Sort by score descending
	sort.Slice(scoredPartners, func(i, j int) bool {
		return scoredPartners[i].score > scoredPartners[j].score
	})

	// Pick top partners with category diversity
	experiences := selectDiverse(scoredPartners, rc, 3)

	title := fmt.Sprintf("%s in %s", rc.TimeSlot, rc.Neighborhood)
	if rc.WeatherCondition != "" {
		title = fmt.Sprintf("%s %s in %s", rc.WeatherCondition, rc.TimeSlot, rc.Neighborhood)
	}

	signalsUsed := []string{}
	for _, s := range rc.SignalsUsed {
		signalsUsed = append(signalsUsed, string(s))
	}
	signalsFailed := []string{}
	for _, s := range rc.SignalsFailed {
		signalsFailed = append(signalsFailed, string(s))
	}

	return models.Recommendation{
		Title:                title,
		Experiences:          experiences,
		TotalDurationHrs:     rc.AvailableHours,
		Source:               "rules",
		ContextSignalsUsed:   signalsUsed,
		ContextSignalsFailed: signalsFailed,
	}
}

func scorePartner(rc *models.RecommendationContext, p partner.Partner) float64 {
	score := 0.0

	// Tag overlap (40% weight)
	overlap := 0
	for _, tag := range rc.SemanticTags {
		for _, pTag := range p.SemanticTags {
			if tag == pTag {
				overlap++
			}
		}
	}
	if len(rc.SemanticTags) > 0 {
		score += 0.4 * float64(overlap) / float64(len(rc.SemanticTags))
	}

	// Distance score (30% weight) — closer is better
	dist := partner.HaversineDistance(rc.Lat, rc.Lon, p.Lat, p.Lon)
	if dist < p.GeoFenceRadiusKm {
		score += 0.3 * (1.0 - dist/p.GeoFenceRadiusKm)
	}

	// Category relevance (30% weight)
	if rc.WeatherTag == "indoor_weather" && (p.Category == "indoor_activity" || p.Category == "wellness" || p.Category == "culture") {
		score += 0.3
	}
	if rc.WeatherTag == "outdoor_weather" && p.Category == "outdoor_activity" {
		score += 0.3
	}
	// Food always scores some points
	if p.Category == "food_and_drink" {
		score += 0.15
	}

	return score
}

// selectDiverse picks partners ensuring category diversity
func selectDiverse(scoredPartners []scored, rc *models.RecommendationContext, max int) []models.Experience {
	var experiences []models.Experience
	usedCategories := map[string]bool{}

	// First pass: one per category
	for _, sp := range scoredPartners {
		if len(experiences) >= max {
			break
		}
		if usedCategories[sp.partner.Category] {
			continue
		}
		usedCategories[sp.partner.Category] = true
		experiences = append(experiences, models.Experience{
			PartnerID:   sp.partner.ID,
			PartnerName: sp.partner.Name,
			Category:    sp.partner.Category,
			Reason:      fmt.Sprintf("Top match for %s (score: %.0f%%)", sp.partner.Category, sp.score*100),
			DistanceKm:  partner.HaversineDistance(rc.Lat, rc.Lon, sp.partner.Lat, sp.partner.Lon),
		})
	}

	// Always try to include parking
	if !usedCategories["parking"] && len(experiences) < max+1 {
		for _, sp := range scoredPartners {
			if sp.partner.Category == "parking" {
				experiences = append(experiences, models.Experience{
					PartnerID:   sp.partner.ID,
					PartnerName: sp.partner.Name,
					Category:    "parking",
					Reason:      "Nearest available parking",
					DistanceKm:  partner.HaversineDistance(rc.Lat, rc.Lon, sp.partner.Lat, sp.partner.Lon),
				})
				break
			}
		}
	}

	return experiences
}
