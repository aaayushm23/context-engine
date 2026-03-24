package models

import "time"

// ContextSignal represents a single enrichment signal
type ContextSignal string

const (
	SignalLocation    ContextSignal = "location"
	SignalWeather     ContextSignal = "weather"
	SignalTime        ContextSignal = "time"
	SignalPreferences ContextSignal = "preferences"
)

// RecommendationRequest is the input from the API client
type RecommendationRequest struct {
	RequestID      string   `json:"request_id,omitempty"`
	Lat            float64  `json:"lat"`
	Lon            float64  `json:"lon"`
	AvailableHours float64  `json:"available_hours"`
	Preferences    []string `json:"preferences"`
}

// RecommendationContext holds the enriched context as it flows through the pipeline
type RecommendationContext struct {
	RequestID    string
	Lat          float64
	Lon          float64
	City         string
	Neighborhood string

	// Weather
	WeatherCondition string  // "rainy", "sunny", "cloudy", "snowy"
	Temperature      float64 // Celsius
	WeatherTag       string  // "indoor_weather", "outdoor_weather"

	// Time
	TimeSlot  string // "weekend_afternoon", "weekday_morning", etc.
	DayOfWeek string
	Hour      int
	Season    string

	// User
	AvailableHours float64
	Preferences    []string

	// Tracking
	SignalsUsed   []ContextSignal
	SignalsFailed []ContextSignal

	// Derived semantic tags for matching
	SemanticTags []string
}

// Experience is a single partner offer within a recommendation
type Experience struct {
	PartnerID   string  `json:"partner_id"`
	PartnerName string  `json:"partner_name"`
	Category    string  `json:"category"`
	Reason      string  `json:"reason"`
	DistanceKm  float64 `json:"distance_km,omitempty"`
}

// Recommendation is the composed output from the decision engine
type Recommendation struct {
	Title                string       `json:"title"`
	Experiences          []Experience `json:"experiences"`
	TotalDurationHrs     float64      `json:"total_duration_hours"`
	Source               string       `json:"source"` // "llm" or "rules"
	ContextSignalsUsed   []string     `json:"context_signals_used"`
	ContextSignalsFailed []string     `json:"context_signals_failed"`
}

// RecommendationResponse is the full API response
type RecommendationResponse struct {
	RequestID      string         `json:"request_id"`
	Recommendation Recommendation `json:"recommendation"`
	Meta           ResponseMeta   `json:"meta"`
}

// ResponseMeta holds performance and debugging info
type ResponseMeta struct {
	LatencyMs int64 `json:"latency_ms"`
	FromCache bool  `json:"from_cache"`
}

// RecommendationEvent is published to Pub/Sub
type RecommendationEvent struct {
	RequestID string         `json:"request_id"`
	EventType string         `json:"event_type"`
	Payload   Recommendation `json:"payload"`
	LatencyMs int64          `json:"latency_ms"`
	Source    string         `json:"source"`
	CreatedAt time.Time      `json:"created_at"`
}
