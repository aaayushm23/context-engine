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

-- Recommendations log
CREATE TABLE IF NOT EXISTS recommendations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id TEXT UNIQUE NOT NULL,
    context_hash TEXT NOT NULL,
    result JSONB NOT NULL,
    source TEXT NOT NULL,
    latency_ms INTEGER NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for semantic tag matching
CREATE INDEX IF NOT EXISTS idx_partners_tags ON partners USING GIN(semantic_tags);
CREATE INDEX IF NOT EXISTS idx_partners_active ON partners(active) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_recommendations_request_id ON recommendations(request_id);