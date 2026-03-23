package partner

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/lib/pq"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// MatchByTags finds partners whose semantic tags overlap with the given tags
// and are within geographic range, ordered by tag relevance
func (r *Repository) MatchByTags(ctx context.Context, tags []string, lat, lon float64, limit int) ([]Partner, error) {
	// We filter by a bounding box first (fast, index-friendly),
	// then rank by semantic tag overlap.
	// The bounding box is ~50km which covers any realistic urban recommendation radius.
	// At scale, this would use PostGIS ST_DWithin with a GiST index for O(log N) lookups.
	const maxDistKm = 50.0
	latDelta := maxDistKm / 111.0 // ~1 degree latitude = 111km
	lonDelta := maxDistKm / (111.0 * math.Cos(lat*math.Pi/180.0))

	query := `
		SELECT id, name, category, semantic_tags, lat, lon, geo_fence_radius_km, active, created_at
		FROM partners
		WHERE active = true
		  AND semantic_tags && $1
		  AND lat BETWEEN $2 AND $3
		  AND lon BETWEEN $4 AND $5
		ORDER BY
			array_length(
				ARRAY(SELECT unnest(semantic_tags) INTERSECT SELECT unnest($1::text[])),
				1
			) DESC NULLS LAST
		LIMIT $6
	`

	rows, err := r.db.QueryContext(ctx, query,
		pq.Array(tags),
		lat-latDelta, lat+latDelta,
		lon-lonDelta, lon+lonDelta,
		limit,
	)
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
		return nil, fmt.Errorf("get all partners: %w", err)
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
		return nil, fmt.Errorf("get partner: %w", err)
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

// HaversineDistance calculates the distance in km between two lat/lon points
func HaversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	lat1R := lat1 * math.Pi / 180
	lat2R := lat2 * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1R)*math.Cos(lat2R)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Asin(math.Sqrt(a))
}
