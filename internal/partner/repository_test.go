package partner

import (
	"math"
	"testing"
)

func TestHaversineDistance(t *testing.T) {
	tests := []struct {
		name    string
		lat1    float64
		lon1    float64
		lat2    float64
		lon2    float64
		wantMin float64 // km
		wantMax float64 // km
	}{
		{
			name: "Berlin Mitte to Kreuzberg (~3.4km)",
			lat1: 52.5200, lon1: 13.4050,
			lat2: 52.4894, lon2: 13.4028,
			wantMin: 3.0, wantMax: 4.0,
		},
		{
			name: "same point returns zero",
			lat1: 52.5200, lon1: 13.4050,
			lat2: 52.5200, lon2: 13.4050,
			wantMin: 0, wantMax: 0.001,
		},
		{
			name: "Berlin to Munich (~504km)",
			lat1: 52.5200, lon1: 13.4050,
			lat2: 48.1351, lon2: 11.5820,
			wantMin: 500, wantMax: 510,
		},
		{
			name: "Berlin Mitte to Charlottenburg (~7.5km)",
			lat1: 52.5200, lon1: 13.4050,
			lat2: 52.5167, lon2: 13.3000,
			wantMin: 7.0, wantMax: 8.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HaversineDistance(tt.lat1, tt.lon1, tt.lat2, tt.lon2)

			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("HaversineDistance() = %.2f km, want between %.2f and %.2f km",
					got, tt.wantMin, tt.wantMax)
			}

			// A mathematically sound great-circle distance algorithm must be commutative.
			reverse := HaversineDistance(tt.lat2, tt.lon2, tt.lat1, tt.lon1)
			if math.Abs(got-reverse) > 0.001 {
				t.Errorf("distance is not symmetric: %.4f vs %.4f", got, reverse)
			}
		})
	}
}
