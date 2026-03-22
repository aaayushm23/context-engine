.PHONY: build run test lint migrate seed demo clean

build:
	go build -o bin/context-engine ./cmd/server

run:
	docker compose up -d postgres redis mosquitto grafana
	@echo "Waiting for services..."
	@sleep 3
	go run ./cmd/server

run-docker:
	docker compose up --build

test:
	go test ./... -v

lint:
	golangci-lint run ./...

migrate:
	PGPASSWORD=secret psql -h localhost -U postgres -d contextengine -f migrations/001_initial.sql

seed:
	PGPASSWORD=secret psql -h localhost -U postgres -d contextengine -f migrations/002_seed_partners.sql

demo:
	@echo "🎯 Sending recommendation request for Berlin, rainy Saturday..."
	@curl -s -X POST http://localhost:8080/v1/recommend \
		-H "Content-Type: application/json" \
		-H "X-Request-ID: demo-001" \
		-d '{"lat": 52.52, "lon": 13.405, "available_hours": 3, "preferences": ["fitness", "food"]}' | jq .

demo-health:
	@curl -s http://localhost:8080/health | jq .

demo-partners:
	@curl -s http://localhost:8080/v1/partners | jq .

demo-metrics:
	@curl -s http://localhost:8080/metrics | jq .

clean:
	docker compose down -v
	rm -rf bin/