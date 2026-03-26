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

// Ping validates the physical connection pool to Postgres. We expose this explicitly
// so orchestration layers (like Kubernetes) can verify the data plane before routing traffic.
func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// MatchByTags finds partners whose semantic tags overlap with the given tags
// and are within geographic range, ordered by tag relevance
func (r *Repository) MatchByTags(ctx context.Context, tags []string, lat, lon float64, limit int) ([]Partner, error) {
	// Bounding box filter first (fast, index-friendly),
	// then rank by semantic tag overlap.
	// ~50km covers any realistic urban recommendation radius.
	// At scale: PostGIS ST_DWithin with GiST index for O(log N) lookups.
	const maxDistKm = 50.0
	latDelta := maxDistKm / 111.0
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

// GetAll provides an administrative escape hatch to dump the current active inventory.
// It bypasses spatial logic entirely, intended only for internal tooling or cache warming.
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

// HaversineDistance provides a mathematically accurate great-circle distance.
// We execute this in the application layer (rather than the DB) to calculate precise
// point-to-point distances for the final LLM prompt, without burdening the database CPU.
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
