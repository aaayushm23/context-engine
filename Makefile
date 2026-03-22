.PHONY: build run test lint migrate seed demo demo-fast demo-failure clean

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
	@bash demo.sh

demo-fast:
	@bash demo-fast.sh

demo-failure:
	@bash demo-failure.sh

demo-health:
	@curl -s http://localhost:8080/health | jq .

demo-partners:
	@curl -s http://localhost:8080/v1/partners | jq .

demo-metrics:
	@curl -s http://localhost:8080/metrics | jq .

clean:
	docker compose down -v
	rm -rf bin/
