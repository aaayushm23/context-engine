# context-engine

A real-time context-aware partner recommendation service built in Go. Given a user's location, weather, time, and preferences, the engine matches against semantically tagged partners, composes bundled experiences using an LLM, and returns personalized recommendations with sub-300ms production latency.

---

## 🏗️ Architecture

```mermaid
flowchart TD

A[Client App]

subgraph API_Layer
B[Router + Middleware]
end

subgraph Context_Pipeline
C[Context Orchestrator]
C1[Weather]
C2[Time]
C3[Location]
C4[Preferences]
end

subgraph Decision_Engine
D[Decision Core]
E[LLM Ollama]
F[Rule Engine]
end

subgraph Data_Layer
G[Partner Service]
H[(PostgreSQL)]
I[(Redis Cache)]
end

subgraph Observability
J[Event Consumer]
K[Grafana]
end

L[MQTT]

A --> B --> C

C --> C1
C --> C2
C --> C3
C --> C4

C --> D

D --> E
D --> F

D --> G --> H
D --> I

I --> J --> K

L --> C
```

---

## Key design decisions

**Graceful degradation over hard failure.** The system always returns a recommendation. If the LLM times out, rule-based scoring takes over. If a context signal fails, the pipeline continues with partial context. The response includes `context_signals_used` and `context_signals_failed` so the client knows what happened.

**Timeout budget propagation.** A total request deadline (300ms production target) is divided across stages via `context.WithTimeout`. Each stage respects its sub-budget. If the LLM exceeds its allocation, context cancels and fallback triggers automatically.

**Interface-based extensibility.** Adding a new data source means writing one file with two methods (`Enrich` and `Name`), then adding one line in `main.go`. The pipeline, circuit breaker, and caching wrap it automatically.

**Idempotency.** Same `request_id` returns the cached result instantly. No duplicate processing, no duplicate Pub/Sub events.

## Tech stack

| Component  | Technology            | Purpose                               |
| ---------- | --------------------- | ------------------------------------- |
| Language   | Go                    | Backend service                       |
| Database   | PostgreSQL 16         | Partner registry, recommendations log |
| Cache      | Redis 7               | Response cache, Pub/Sub, idempotency  |
| LLM        | Ollama (Llama 3.1 8B) | Experience composition                |
| IoT        | MQTT (Mosquitto)      | Vehicle/sensor signal ingestion       |
| Monitoring | Grafana               | Observability dashboard               |
| Infra      | Docker Compose        | Full local stack                      |

## Quickstart

### Prerequisites

* Go 1.22+
* Docker Desktop
* Ollama (`brew install ollama`)
* jq (`brew install jq`)
* psql (`brew install libpq && brew link --force libpq`)

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

| Header          | Description                                            |
| --------------- | ------------------------------------------------------ |
| `X-Request-ID`  | Client-provided request ID (auto-generated if missing) |
| `X-Force-Rules` | Set to `true` to skip LLM, use rule-based scoring      |

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

| Variable     | Default                                                                 |
| ------------ | ----------------------------------------------------------------------- |
| DATABASE_URL | postgres://postgres:secret@localhost:5432/contextengine?sslmode=disable |
| REDIS_ADDR   | localhost:6379                                                          |
| OLLAMA_URL   | http://localhost:11434                                                  |
| OLLAMA_MODEL | llama3.1:8b                                                             |
| PORT         | 8080                                                                    |

## Scaling to production — what I'd change and why
 
The current implementation is designed to be correct, testable, and demonstrably resilient. Below are the architectural decisions where it makes deliberate tradeoffs, and the enterprise-grade alternatives I would adopt at scale.
 
### 1. Geospatial: Haversine → PostGIS
 
**Current approach:** Partners are fetched from Postgres and distances are calculated using the Haversine formula in Go. This works well for the current dataset of 15 Berlin-area partners.
 
**At scale:** With hundreds of thousands of partners, computing distance for every row becomes an O(N) CPU bottleneck. PostGIS extends Postgres with spatial indexes (GiST), enabling O(log N) nearest-neighbor lookups. A query like `ST_DWithin(geom, ST_MakePoint(13.405, 52.52)::geography, 5000)` finds all partners within 5km using the spatial index, without scanning the full table.
 
### 2. LLM: Synchronous call → pre-computed semantic search
 
**Current approach:** The LLM is invoked synchronously during the HTTP request. If it exceeds the timeout budget, the system falls back to rule-based scoring. This guarantees a response but limits LLM reasoning time.
 
**At scale:** Getting a local 8B parameter model to produce structured JSON within 300ms is unreliable. A better enterprise approach is to pre-compute recommendation bundles offline. The LLM runs in the background continuously, generating experience bundles like "A perfect rainy day in Berlin-Mitte" and converting them into vector embeddings. At request time, the user's context is embedded and a vector database (pgvector or Pinecone) performs a cosine similarity lookup in under 10ms. This gives LLM-quality reasoning with database-speed latency.
 
### 3. Events: Redis Pub/Sub → Redis Streams or Kafka
 
**Current approach:** Analytics events are published via Redis Pub/Sub to a background consumer. This is simple and fast, but Pub/Sub is fire-and-forget — if the consumer is offline when an event is published, that event is permanently lost.
 
**At scale:** Redis Streams (or Kafka, RabbitMQ) provide message persistence and consumer groups. If the analytics consumer crashes and restarts 30 seconds later, it picks up exactly where it left off. No lost events, no gaps in metrics. The API stays identical: `XADD` instead of `PUBLISH`.
 
### 4. Response delivery: Synchronous HTTP → Server-Sent Events
 
**Current approach:** The HTTP request blocks until the pipeline completes or the timeout triggers. With the rule-based path this is fast (~200ms), but the LLM path can take several seconds locally.
 
**At scale:** For highly personalized AI recommendations, users are typically willing to wait 2-3 seconds as long as they see progress. Server-Sent Events (SSE) allow the backend to stream status updates as each pipeline stage completes: `{"status": "Fetching weather..."}` → `{"status": "Matching partners..."}` → `{"status": "AI composing experience..."}` → `{"result": {...}}`. This improves perceived performance and gives the LLM the time it needs.
 
### 5. Observability: Structured logs → OpenTelemetry
 
**Current approach:** Every request is logged with `log/slog` carrying a `request_id` for traceability. This is solid for debugging individual requests.
 
**At scale:** With dozens of concurrent goroutines, timeouts, and fallbacks, structured logs alone make it hard to visualize where time is spent. OpenTelemetry distributed tracing would produce a Gantt chart for each request, showing exactly how many milliseconds the weather API, partner matching, and LLM each consumed. Combined with Grafana Tempo, this makes performance debugging trivial — you can see at a glance that the weather enricher took 95ms while the LLM was cancelled at 120ms.
 
## License
 
MIT
 
