package partner

import "time"

// Partner establishes the core domain entity for supply-side inventory.
// Its fields are heavily optimized for rapid JSON serialization and PostGIS bounding-box queries.
type Partner struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Category          string                 `json:"category"`
	SemanticTags      []string               `json:"semantic_tags"`
	Lat               float64                `json:"lat"`
	Lon               float64                `json:"lon"`
	GeoFenceRadiusKm  float64                `json:"geo_fence_radius_km"`
	AvailabilityRules map[string]interface{} `json:"availability_rules,omitempty"`
	Active            bool                   `json:"active"`
	CreatedAt         time.Time              `json:"created_at"`
}
