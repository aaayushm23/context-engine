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

type LocationEnricher struct{}

func NewLocationEnricher() *LocationEnricher { return &LocationEnricher{} }

func (l *LocationEnricher) Name() string { return "location" }

func (l *LocationEnricher) Enrich(ctx context.Context, rc *models.RecommendationContext) error {
	// Simple reverse geocoding based on known Berlin neighborhoods
	// In production, this would call a geocoding API
	rc.City = classifyCity(rc.Lat, rc.Lon)
	rc.Neighborhood = classifyNeighborhood(rc.Lat, rc.Lon)
	return nil
}

func classifyCity(lat, lon float64) string {
	// Berlin bounding box (rough)
	if lat >= 52.3 && lat <= 52.7 && lon >= 13.1 && lon <= 13.8 {
		return "Berlin"
	}
	if lat >= 48.0 && lat <= 48.3 && lon >= 11.3 && lon <= 11.8 {
		return "Munich"
	}
	return "Unknown"
}

func classifyNeighborhood(lat, lon float64) string {
	// Known Berlin neighborhoods with rough center coordinates
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
