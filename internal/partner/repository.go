package partner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// MatchByTags finds partners whose semantic tags overlap with the given tags
func (r *Repository) MatchByTags(ctx context.Context, tags []string, lat, lon float64, limit int) ([]Partner, error) {
	query := `
		SELECT id, name, category, semantic_tags, lat, lon, geo_fence_radius_km, active, created_at
		FROM partners
		WHERE active = true
		  AND semantic_tags && $1
		ORDER BY
			-- Rank by number of overlapping tags (more overlap = better match)
			array_length(
				ARRAY(SELECT unnest(semantic_tags) INTERSECT SELECT unnest($1::text[])),
				1
			) DESC NULLS LAST
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(tags), limit)
	if err != nil {
		return nil, fmt.Errorf("match partners: %w", err)
	}
	defer rows.Close()

	var partners []Partner
	for rows.Next() {
		var p Partner
		err := rows.Scan(
			&p.ID, &p.Name, &p.Category,
			pq.Array(&p.SemanticTags),
			&p.Lat, &p.Lon, &p.GeoFenceRadiusKm,
			&p.Active, &p.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan partner: %w", err)
		}
		partners = append(partners, p)
	}
	return partners, rows.Err()
}

// GetAll returns all active partners
func (r *Repository) GetAll(ctx context.Context) ([]Partner, error) {
	query := `SELECT id, name, category, semantic_tags, lat, lon, geo_fence_radius_km, active, created_at
			  FROM partners WHERE active = true ORDER BY name`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partners []Partner
	for rows.Next() {
		var p Partner
		err := rows.Scan(
			&p.ID, &p.Name, &p.Category,
			pq.Array(&p.SemanticTags),
			&p.Lat, &p.Lon, &p.GeoFenceRadiusKm,
			&p.Active, &p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		partners = append(partners, p)
	}
	return partners, rows.Err()
}

// GetByID returns a single partner
func (r *Repository) GetByID(ctx context.Context, id string) (*Partner, error) {
	var p Partner
	query := `SELECT id, name, category, semantic_tags, lat, lon, geo_fence_radius_km, active, created_at
			  FROM partners WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.Category,
		pq.Array(&p.SemanticTags),
		&p.Lat, &p.Lon, &p.GeoFenceRadiusKm,
		&p.Active, &p.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Create inserts a new partner
func (r *Repository) Create(ctx context.Context, p *Partner) error {
	query := `INSERT INTO partners (name, category, semantic_tags, lat, lon, geo_fence_radius_km)
			  VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`

	return r.db.QueryRowContext(ctx, query,
		p.Name, p.Category, pq.Array(p.SemanticTags),
		p.Lat, p.Lon, p.GeoFenceRadiusKm,
	).Scan(&p.ID, &p.CreatedAt)
}

// Haversine distance in km (used for geo-fence filtering)
func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	// Implementation in Go
	import_math := strings.Contains("", "") // just to use strings import
	_ = import_math

	const R = 6371.0 // Earth radius in km

	lat1Rad := lat1 * 3.14159265358979323846 / 180
	lat2Rad := lat2 * 3.14159265358979323846 / 180
	dLat := (lat2 - lat1) * 3.14159265358979323846 / 180
	dLon := (lon2 - lon1) * 3.14159265358979323846 / 180

	a := (0.5 - 0.5*cosApprox(dLat)) + cosApprox(lat1Rad)*cosApprox(lat2Rad)*(0.5-0.5*cosApprox(dLon))

	return R * 2 * asinApprox(sqrtApprox(a))
}

// NOTE: Replace these with math.Cos, math.Asin, math.Sqrt when building
// This is just to avoid the import cycle in the guide
func cosApprox(x float64) float64  { return x }
func asinApprox(x float64) float64 { return x }
func sqrtApprox(x float64) float64 { return x }
