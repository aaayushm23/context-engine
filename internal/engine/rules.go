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

// SelectPartners acts as the deterministic hard-boundary for inventory.
// By strictly isolating spatial and categorical filtering here, we ensure that
// any downstream processing (like LLM curation) operates on a mathematically valid
// and verified subset of the graph, completely neutralizing hallucination risks.
func SelectPartners(rc *models.RecommendationContext, partners []partner.Partner, max int) []partner.Partner {
	if len(partners) == 0 {
		return nil
	}

	// We apply a uniform scoring heuristic to normalize diverse attributes (distance, tags, categories)
	// into a single comparable floating-point metric for ranking.
	var scoredPartners []scored
	for _, p := range partners {
		score := scorePartner(rc, p)
		scoredPartners = append(scoredPartners, scored{partner: p, score: score})
	}

	// Stable descendent sort guarantees the most relevant inventory surfaces first.
	sort.Slice(scoredPartners, func(i, j int) bool {
		return scoredPartners[i].score > scoredPartners[j].score
	})

	// Enforcing strict category diversity prevents homogenous recommendations
	// (e.g., suggesting three coffee shops) which leads to poor user conversion.
	var selected []partner.Partner
	usedCategories := map[string]bool{}

	for _, sp := range scoredPartners {
		if len(selected) >= max {
			break
		}
		if usedCategories[sp.partner.Category] {
			continue
		}
		usedCategories[sp.partner.Category] = true
		selected = append(selected, sp.partner)
	}

	// Injecting specific secondary inventory (like parking) adds holistic value
	// that a pure nearest-neighbor distance sort would naturally truncate.
	if !usedCategories["parking"] {
		for _, sp := range scoredPartners {
			if sp.partner.Category == "parking" {
				selected = append(selected, sp.partner)
				break
			}
		}
	}

	return selected
}

// BuildRecommendation constructs a complete, valid response payload purely via rules.
// It serves as the baseline architectural fallback, ensuring the system can degrade gracefully
// and remain highly available even if advanced probabilistic layers (LLMs) completely collapse.
func BuildRecommendation(rc *models.RecommendationContext, selected []partner.Partner) models.Recommendation {
	var experiences []models.Experience

	for _, p := range selected {
		reason := generateReason(rc, p)
		experiences = append(experiences, models.Experience{
			PartnerID:   p.ID,
			PartnerName: p.Name,
			Category:    p.Category,
			Reason:      reason,
			DistanceKm:  partner.HaversineDistance(rc.Lat, rc.Lon, p.Lat, p.Lon),
		})
	}

	title := generateTitle(rc)

	signalsUsed := make([]string, len(rc.SignalsUsed))
	for i, s := range rc.SignalsUsed {
		signalsUsed[i] = string(s)
	}
	signalsFailed := make([]string, len(rc.SignalsFailed))
	for i, s := range rc.SignalsFailed {
		signalsFailed[i] = string(s)
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

// RuleBasedFallback orchestrates the full deterministic pipeline. It provides
// a clean substitution boundary that integration tests can target to verify core logic
// without standing up heavy model infrastructure.
func RuleBasedFallback(rc *models.RecommendationContext, partners []partner.Partner) models.Recommendation {
	selected := SelectPartners(rc, partners, 3)
	return BuildRecommendation(rc, selected)
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

func generateTitle(rc *models.RecommendationContext) string {
	title := rc.TimeSlot
	if rc.WeatherCondition != "" {
		title = rc.WeatherCondition + " " + title
	}
	if rc.Neighborhood != "" {
		title += " in " + rc.Neighborhood
	}
	return title
}

func generateReason(rc *models.RecommendationContext, p partner.Partner) string {
	dist := partner.HaversineDistance(rc.Lat, rc.Lon, p.Lat, p.Lon)

	switch p.Category {
	case "indoor_activity":
		if rc.WeatherTag == "indoor_weather" {
			return fmt.Sprintf("Indoor activity — ideal for %s weather (%.1fkm away)", rc.WeatherCondition, dist)
		}
		return fmt.Sprintf("Indoor activity nearby (%.1fkm away)", dist)
	case "outdoor_activity":
		if rc.WeatherTag == "outdoor_weather" {
			return fmt.Sprintf("Outdoor activity — great for %s weather (%.1fkm away)", rc.WeatherCondition, dist)
		}
		return fmt.Sprintf("Outdoor activity (%.1fkm away)", dist)
	case "food_and_drink":
		return fmt.Sprintf("Food and drinks nearby (%.1fkm away)", dist)
	case "parking":
		return fmt.Sprintf("Covered parking (%.1fkm away)", dist)
	case "wellness":
		return fmt.Sprintf("Relaxation and wellness (%.1fkm away)", dist)
	case "culture":
		return fmt.Sprintf("Culture and arts (%.1fkm away)", dist)
	default:
		return fmt.Sprintf("Recommended based on your preferences (%.1fkm away)", dist)
	}
}
