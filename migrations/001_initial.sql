-- Partners table
CREATE TABLE IF NOT EXISTS partners (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    semantic_tags TEXT[] NOT NULL,
    lat DOUBLE PRECISION NOT NULL,
    lon DOUBLE PRECISION NOT NULL,
    geo_fence_radius_km DOUBLE PRECISION DEFAULT 5.0,
    availability_rules JSONB DEFAULT '{}',
    active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for semantic tag matching (GIN for array overlap operator &&)
CREATE INDEX IF NOT EXISTS idx_partners_tags ON partners USING GIN(semantic_tags);

-- Index for bounding box queries on lat/lon (used by MatchByTags)
CREATE INDEX IF NOT EXISTS idx_partners_location ON partners(lat, lon) WHERE active = true;

-- Index for active partner filtering
CREATE INDEX IF NOT EXISTS idx_partners_active ON partners(active) WHERE active = true;
