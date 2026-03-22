# context-engine

A real-time context-aware partner recommendation service built in Go. Given a user's location, weather, time, and preferences, the engine matches against semantically tagged partners, composes bundled experiences using an LLM, and returns personalized recommendations with sub-300ms production latency.

## Architecture

```
Context Signals (GPS, Weather, Time, Preferences, Parking)
           │
           ▼
┌─────────────────────────────────┐
│   Resilience Layer              │
│   Circuit breakers · Bulkhead   │
│   Retry · Partial response      │
└──────────────┬──────────────────┘
           │
           ▼
┌─────────────────────────────────┐
│   Cache Layer (Redis)           │
│   Weather: 15m · LLM: 30m TTL  │
└──────────────┬──────────────────┘
           │
           ▼
┌─────────────────────────────────────────────────┐
│  context-engine  (Go · cmd/ internal/ pkg/)     │
│                                                 │
│  ┌───────────────────────────────────────────┐  │
│  │ Request Tracking                          │  │
│  │ request_id · idempotency · timeout budget │  │
│  └───────────────────┬───────────────────────┘  │
│                      │                          │
│  ┌───────────────────▼───────────────────────┐  │
│  │ Context Pipeline (parallel enrichment)    │  │
│  │ Location → Weather → Time → Preferences   │  │
│  └──────┬────────────────────────┬───────────┘  │
│         │                        │              │
│  ┌──────▼──────────┐  ┌─────────▼───────────┐  │
│  │ Postgres         │  │ LLM (Ollama)        │  │
│  │ Semantic tags    │  │ + Rule-based        │  │
│  │ Partner matching │  │   fallback          │  │
│  └──────┬──────────┘  └─────────┬───────────┘  │
│         │                        │              │
│  ┌──────▼────────────────────────▼───────────┐  │
│  │ Decision Engine                           │  │
│  │ Rank · Bundle · Deduplicate · Idempotency │  │
│  └──────┬────────────────────────┬───────────┘  │
│         │                        │              │
│    /v1/recommend            Redis Pub/Sub       │
│    /v1/partners             Events → Analytics  │
│    /health · /metrics                           │
└─────────────────────────────────────────────────┘
```

## Key design decisions

**Graceful degradation over hard failure.** The system always returns a recommendation. If the LLM times out, rule-based scoring takes over. If a context signal fails, the pipeline continues with partial context. The response includes `context_signals_used` and `context_signals_failed` so the client knows what happened.

**Timeout budget propagation.** A total request deadline (300ms production target) is divided across stages via `context.WithTimeout`. Each stage respects its sub-budget. If the LLM exceeds its allocation, context cancels and fallback triggers automatically.

**Interface-based extensibility.** Adding a new data source means writing one file with two methods (`Enrich` and `Name`), then adding one line in `main.go`. The pipeline, circuit breaker, and caching wrap it automatically.

**Idempotency.** Same `request_id` returns the cached result instantly. No duplicate processing, no duplicate Pub/Sub events.

## Tech stack

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Language | Go | Backend service |
| Database | PostgreSQL 16 | Partner registry, recommendations log |
| Cache | Redis 7 | Response cache, Pub/Sub, idempotency |
| LLM | Ollama (Llama 3.1 8B) | Experience composition |
| IoT | MQTT (Mosquitto) | Vehicle/sensor signal ingestion |
| Monitoring | Grafana | Observability dashboard |
| Infra | Docker Compose | Full local stack |

## Quickstart

### Prerequisites

- Go 1.22+
- Docker Desktop
- Ollama (`brew install ollama`)
- jq (`brew install jq`)
- psql (`brew install libpq && brew link --force libpq`)

### Setup

```bash
# Clone
git clone https://github.com/aaayushm23/context-engine.git
cd context-engine

# Pull the LLM model
ollama serve &
ollama pull llama3.1:8b

# Start infrastructure
docker compose up -d postgres redis mosquitto grafana

# Run migrations and seed Berlin partner data
make migrate
make seed

# Start the API
make run
```

### Run demos

```bash
# Production-speed demo (rule-based, ~200ms)
make demo-fast

# Failure simulation (LLM unavailable, graceful degradation)
make demo-failure

# Full LLM demo (intelligent recommendations + idempotency test)
make demo
```

### Run tests

```bash
go test ./... -v
```

## API reference

### POST /v1/recommend

Generate a context-aware recommendation.

**Request:**
```json
{
  "lat": 52.52,
  "lon": 13.405,
  "available_hours": 3,
  "preferences": ["fitness", "food"]
}
```

**Headers:**
- `X-Request-ID` — client-provided request ID (auto-generated if missing)
- `X-Force-Rules: true` — skip LLM, use rule-based scoring (for testing)

**Response:**
```json
{
  "request_id": "demo-001",
  "recommendation": {
    "title": "Berlin Active Evening",
    "experiences": [
      {
        "partner_name": "Urban Sports Club Mitte",
        "category": "indoor_activity",
        "reason": "Indoor fitness, perfect for the evening",
        "distance_km": 0.58
      },
      {
        "partner_name": "Burgermeister",
        "category": "food_and_drink",
        "reason": "Quick bite after your workout",
        "distance_km": 3.38
      }
    ],
    "source": "llm",
    "context_signals_used": ["weather", "time", "location", "preferences"],
    "context_signals_failed": []
  },
  "meta": {
    "latency_ms": 187,
    "cache_hits": [],
    "from_cache": false
  }
}
```

### GET /v1/partners

List all registered partners.

### GET /health

Service health check (Postgres, Redis connectivity).

### GET /metrics

Runtime metrics: total recommendations, LLM vs fallback ratio, average latency.

## Project structure

```
context-engine/
├── cmd/server/main.go              # Entry point, dependency injection, graceful shutdown
├── internal/
│   ├── api/
│   │   ├── handler.go              # HTTP handlers (/recommend, /partners, /health)
│   │   ├── middleware.go           # Request ID, logging, timeout, panic recovery
│   │   ├── router.go              # Route registration, versioned endpoints
│   │   └── middleware_test.go
│   ├── context/
│   │   ├── pipeline.go            # Parallel enrichment orchestration
│   │   ├── enrichers.go           # Weather, time, location, preference enrichers
│   │   └── enrichers_test.go
│   ├── partner/
│   │   ├── models.go              # Partner struct
│   │   ├── repository.go          # Postgres queries, semantic tag matching
│   │   └── repository_test.go
│   ├── engine/
│   │   ├── decision.go            # Decision engine (LLM + fallback + idempotency)
│   │   ├── rules.go               # Rule-based scoring (tag overlap, distance, diversity)
│   │   ├── decision_test.go       # Failure tests (timeout, garbage JSON, server down)
│   │   └── rules_test.go
│   ├── resilience/
│   │   ├── circuitbreaker.go      # Circuit breaker (closed → open → half-open)
│   │   ├── retry.go               # Exponential backoff
│   │   ├── timeout.go             # Budget manager (sub-budgets per stage)
│   │   ├── circuitbreaker_test.go
│   │   └── timeout_test.go
│   ├── cache/
│   │   └── redis.go               # Cache + idempotency + content-hash keys
│   ├── events/
│   │   ├── publisher.go           # Pub/Sub event publishing
│   │   └── consumer.go            # Analytics consumer (latency, hit rate)
│   ├── llm/
│   │   └── ollama.go              # Ollama client, structured prompting, JSON parsing
│   └── mqtt/
│       └── listener.go            # MQTT subscriber for IoT signals
├── pkg/models/
│   └── recommendation.go          # Shared types (request, response, events)
├── migrations/
│   ├── 001_initial.sql            # Schema (partners, recommendations, indexes)
│   └── 002_seed_partners.sql      # 15 Berlin-area partners
├── asyncapi/
│   └── spec.yaml                  # Event contract documentation
├── demo.sh                        # Full LLM demo with idempotency test
├── demo-fast.sh                   # Rule-based demo (~200ms)
├── demo-failure.sh                # Failure simulation demo
├── docker-compose.yml             # Postgres + Redis + Mosquitto + Grafana
├── Dockerfile                     # Multi-stage build
├── Makefile                       # build, run, test, migrate, seed, demo
└── .env.example                   # Configuration reference
```

## Testing

30 tests covering unit logic, failure scenarios, and integration:

```
ok   internal/api          — middleware (request ID, panic recovery)
ok   internal/context      — enrichers (time, location, preferences, weather codes)
ok   internal/engine       — decision engine failures + rule-based scoring
ok   internal/partner      — Haversine distance (accuracy + symmetry)
ok   internal/resilience   — circuit breaker + timeout budget
```

**Failure tests** (the most important ones):
- LLM timeout → verify fallback returns valid recommendation
- LLM returns garbage JSON → system doesn't crash, falls back
- LLM completely unreachable → connection error caught, fallback works
- Circuit breaker → after 2 failures, 3rd call never reaches server
- Partial context → weather fails, recommendation still returned with degraded personalization

```bash
# Run all tests
go test ./... -v

# Run only failure tests
go test ./internal/engine/... -v -run "Fallback"

# Run with race detection
go test ./... -race

# Run with coverage
go test ./... -cover
```

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://postgres:secret@localhost:5432/contextengine?sslmode=disable` | Postgres connection |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama API endpoint |
| `OLLAMA_MODEL` | `llama3.1:8b` | LLM model name |
| `PORT` | `8080` | HTTP server port |

## What I'd improve with more time

- Prometheus metrics + OpenTelemetry distributed tracing
- PostGIS for real geo-fencing instead of Haversine approximation
- Rate limiting per API key
- Expand failure tests for concurrency edge cases
- WebSocket endpoint for streaming recommendations
- Partner availability windows (time-based filtering)

## License

MIT
