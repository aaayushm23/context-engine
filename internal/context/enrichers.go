package context

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/aaayushm23/context-engine/pkg/models"
)

// --- Weather Enricher ---

type WeatherEnricher struct {
	httpClient *http.Client
}

func NewWeatherEnricher() *WeatherEnricher {
	return &WeatherEnricher{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (w *WeatherEnricher) Name() string { return "weather" }

func (w *WeatherEnricher) Enrich(ctx context.Context, rc *models.RecommendationContext) error {
	// Open-Meteo is free, no API key needed
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,weather_code",
		rc.Lat, rc.Lon,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			WeatherCode int     `json:"weather_code"`
		} `json:"current"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	rc.Temperature = result.Current.Temperature
	rc.WeatherCondition = classifyWeatherCode(result.Current.WeatherCode)

	// Tag for semantic matching
	if isIndoorWeather(result.Current.WeatherCode) {
		rc.WeatherTag = "indoor_weather"
	} else {
		rc.WeatherTag = "outdoor_weather"
	}

	return nil
}

func classifyWeatherCode(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code <= 3:
		return "cloudy"
	case code <= 49:
		return "foggy"
	case code <= 69:
		return "rainy"
	case code <= 79:
		return "snowy"
	case code <= 99:
		return "stormy"
	default:
		return "unknown"
	}
}

func isIndoorWeather(code int) bool {
	// Rain, snow, storm → indoor
	return code >= 50
}

// --- Time Enricher ---

type TimeEnricher struct{}

func NewTimeEnricher() *TimeEnricher { return &TimeEnricher{} }

func (t *TimeEnricher) Name() string { return "time" }

func (t *TimeEnricher) Enrich(ctx context.Context, rc *models.RecommendationContext) error {
	now := time.Now()
	rc.DayOfWeek = now.Weekday().String()
	rc.Hour = now.Hour()

	// Season
	month := now.Month()
	switch {
	case month >= 3 && month <= 5:
		rc.Season = "spring"
	case month >= 6 && month <= 8:
		rc.Season = "summer"
	case month >= 9 && month <= 11:
		rc.Season = "autumn"
	default:
		rc.Season = "winter"
	}

	// Time slot
	isWeekend := now.Weekday() == time.Saturday || now.Weekday() == time.Sunday
	hour := now.Hour()

	switch {
	case isWeekend && hour < 12:
		rc.TimeSlot = "weekend_morning"
	case isWeekend && hour < 17:
		rc.TimeSlot = "weekend_afternoon"
	case isWeekend:
		rc.TimeSlot = "weekend_evening"
	case hour < 12:
		rc.TimeSlot = "weekday_morning"
	case hour < 17:
		rc.TimeSlot = "weekday_afternoon"
	default:
		rc.TimeSlot = "weekday_evening"
	}

	return nil
}

// --- Location Enricher ---
//
// Uses the Nominatim reverse geocoding API (OpenStreetMap).
// Free, no API key required. Nominatim usage policy: max 1 req/sec,
// must include a descriptive User-Agent.
//
// Falls back to bounding-box classification if the API call fails —
// consistent with the rest of the pipeline: always return something.

type nominatimResponse struct {
	Address struct {
		Suburb      string `json:"suburb"`
		CityDistrict string `json:"city_district"`
		City        string `json:"city"`
		Town        string `json:"town"`
		Village     string `json:"village"`
		Country     string `json:"country"`
	} `json:"address"`
}

type LocationEnricher struct {
	httpClient *http.Client
}

func NewLocationEnricher() *LocationEnricher {
	return &LocationEnricher{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (l *LocationEnricher) Name() string { return "location" }

func (l *LocationEnricher) Enrich(ctx context.Context, rc *models.RecommendationContext) error {
	city, neighborhood, err := l.reverseGeocode(ctx, rc.Lat, rc.Lon)
	if err != nil {
		// Graceful degradation: fall back to bounding-box classification.
		// This mirrors the circuit breaker philosophy — partial context is
		// better than no recommendation at all.
		rc.City = classifyCity(rc.Lat, rc.Lon)
		rc.Neighborhood = classifyNeighborhood(rc.Lat, rc.Lon)
		return err
	}
	rc.City = city
	rc.Neighborhood = neighborhood
	return nil
}

// reverseGeocode calls the Nominatim OpenStreetMap API to resolve
// real city and neighborhood names from GPS coordinates.
func (l *LocationEnricher) reverseGeocode(ctx context.Context, lat, lon float64) (city, neighborhood string, err error) {
	url := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?format=json&lat=%.6f&lon=%.6f",
		lat, lon,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", err
	}
	// Nominatim requires a meaningful User-Agent identifying your app
	req.Header.Set("User-Agent", "context-engine/1.0 (github.com/aaayushm23/context-engine)")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("nominatim request failed: %w", err)
	}
	defer resp.Body.Close()

	var result nominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("nominatim decode failed: %w", err)
	}

	// Resolve city: prefer city, fall back to town or village
	city = result.Address.City
	if city == "" {
		city = result.Address.Town
	}
	if city == "" {
		city = result.Address.Village
	}
	if city == "" {
		city = "Unknown"
	}

	// Resolve neighborhood: prefer suburb, fall back to city_district
	neighborhood = result.Address.Suburb
	if neighborhood == "" {
		neighborhood = result.Address.CityDistrict
	}
	if neighborhood == "" {
		neighborhood = city // last resort
	}

	return city, neighborhood, nil
}

// classifyCity and classifyNeighborhood are kept as fallback logic
// when the Nominatim API is unavailable.
func classifyCity(lat, lon float64) string {
	if lat >= 52.3 && lat <= 52.7 && lon >= 13.1 && lon <= 13.8 {
		return "Berlin"
	}
	if lat >= 48.0 && lat <= 48.3 && lon >= 11.3 && lon <= 11.8 {
		return "Munich"
	}
	return "Unknown"
}

func classifyNeighborhood(lat, lon float64) string {
	neighborhoods := map[string][2]float64{
		"Mitte":           {52.5200, 13.4050},
		"Kreuzberg":       {52.4894, 13.4028},
		"Prenzlauer Berg": {52.5388, 13.4244},
		"Friedrichshain":  {52.5159, 13.4539},
		"Charlottenburg":  {52.5167, 13.3000},
		"Neukölln":        {52.4812, 13.4348},
	}
	closest := "Unknown"
	minDist := math.MaxFloat64
	for name, coords := range neighborhoods {
		dist := math.Sqrt(math.Pow(lat-coords[0], 2) + math.Pow(lon-coords[1], 2))
		if dist < minDist {
			minDist = dist
			closest = name
		}
	}
	return closest
}

// --- Preference Enricher ---

type PreferenceEnricher struct{}

func NewPreferenceEnricher() *PreferenceEnricher { return &PreferenceEnricher{} }

func (p *PreferenceEnricher) Name() string { return "preferences" }

func (p *PreferenceEnricher) Enrich(ctx context.Context, rc *models.RecommendationContext) error {
	// In production, this would load from user profile DB
	// For now, preferences come from the request directly
	// This enricher could expand shorthand preferences into full tags
	expanded := []string{}
	for _, pref := range rc.Preferences {
		switch pref {
		case "fitness":
			expanded = append(expanded, "fitness", "sports", "active", "gym", "bouldering")
		case "food":
			expanded = append(expanded, "food", "restaurant", "cafe", "food_and_drink")
		case "culture":
			expanded = append(expanded, "culture", "museum", "gallery", "theater")
		case "outdoor":
			expanded = append(expanded, "outdoor", "park", "nature", "hiking")
		case "shopping":
			expanded = append(expanded, "shopping", "retail", "market")
		default:
			expanded = append(expanded, pref)
		}
	}
	rc.Preferences = expanded
	return nil
}
