package models

import "time"

// ContextSignal defines the rigid vocabulary of measurable environmental factors.
// Using a strong type ensures we don't accidentally pass raw strings into the tracking layer.
type ContextSignal string

const (
	SignalLocation    ContextSignal = "location"
	SignalWeather     ContextSignal = "weather"
	SignalTime        ContextSignal = "time"
	SignalPreferences ContextSignal = "preferences"
)

// RecommendationRequest is the immutable input contract mapped directly from the HTTP payload.
type RecommendationRequest struct {
	RequestID      string   `json:"request_id,omitempty"`
	Lat            float64  `json:"lat"`
	Lon            float64  `json:"lon"`
	AvailableHours float64  `json:"available_hours"`
	Preferences    []string `json:"preferences"`
}

// RecommendationContext acts as the centralized "knowledge graph" for a single request.
// It mutates as it flows through the pipeline, aggregating data from disparate enrichers
// into a single cohesive state that the decision engine can reason about.
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

// Experience projects a complex internal Partner database record into an optimized
// semantic shape suitable for JSON serialization and prompt construction.
type Experience struct {
	PartnerID   string  `json:"partner_id"`
	PartnerName string  `json:"partner_name"`
	Category    string  `json:"category"`
	Reason      string  `json:"reason"`
	DistanceKm  float64 `json:"distance_km,omitempty"`
}

// Recommendation encapsulates the resolved business output, completely decoupled
// from the HTTP transport layer but heavily decorated with observability metadata.
type Recommendation struct {
	Title                string       `json:"title"`
	Experiences          []Experience `json:"experiences"`
	TotalDurationHrs     float64      `json:"total_duration_hours"`
	Source               string       `json:"source"` // "llm" or "rules"
	ContextSignalsUsed   []string     `json:"context_signals_used"`
	ContextSignalsFailed []string     `json:"context_signals_failed"`
}

// RecommendationResponse defines the strict outward-facing API contract.
type RecommendationResponse struct {
	RequestID      string         `json:"request_id"`
	Recommendation Recommendation `json:"recommendation"`
	Meta           ResponseMeta   `json:"meta"`
}

// ResponseMeta separates core business data from operational telemetry,
// allowing clients to blindly parse the payload while debugging tools inspect the envelope.
type ResponseMeta struct {
	LatencyMs int64 `json:"latency_ms"`
	FromCache bool  `json:"from_cache"`
}

// RecommendationEvent represents an asynchronous domain event. It structurally separates
// the synchronous user flow from delayed background processing like billing or ML training.
type RecommendationEvent struct {
	RequestID string         `json:"request_id"`
	EventType string         `json:"event_type"`
	Payload   Recommendation `json:"payload"`
	LatencyMs int64          `json:"latency_ms"`
	Source    string         `json:"source"`
	CreatedAt time.Time      `json:"created_at"`
}
