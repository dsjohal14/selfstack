.PHONY: api api-dev worker tidy fmt test lint precommit migrate db-up db-down test-wal

# Production mode (default): WAL + Postgres + Compaction
api:
	@echo "Starting API with WAL + Postgres (production mode)..."
	DATABASE_URL=postgres://selfstack:selfstack@localhost:5432/selfstack?sslmode=disable \
	go run ./cmd/api

# Development mode: WAL with in-memory manifest (no Postgres required)
api-dev:
	@echo "Starting API with WAL (dev mode, no Postgres)..."
	go run ./cmd/api

# Legacy mode: File-based storage (testing only)
api-legacy:
	@echo "Starting API with legacy file storage..."
	WAL_DISABLED=true go run ./cmd/api

# Background worker
worker: ; go run ./cmd/worker

# Code quality
tidy:  ; go mod tidy
fmt:   ; gofmt -s -w .
test:  ; go test ./... -v -count=1
lint:  ; golangci-lint run

# Database
db-up:
	docker compose -f ops/docker-compose.yml up -d
	@echo "Waiting for Postgres to be ready..."
	@sleep 5
	@echo "Running migrations..."
	cat migrations/0001_init.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack 2>/dev/null || true
	cat migrations/0002_wal_segments.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack 2>/dev/null || true
	cat migrations/0003_segment_type.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack 2>/dev/null || true
	@echo "Database ready!"

db-down:
	docker compose -f ops/docker-compose.yml down

migrate:
	cat migrations/0001_init.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack
	cat migrations/0002_wal_segments.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack
	cat migrations/0003_segment_type.sql | docker exec -i selfstack-db psql -U selfstack -d selfstack

# Run all pre-commit checks
precommit: fmt tidy lint test
	@echo "All pre-commit checks passed!"

# WAL integration test (100 events, crash recovery, compaction, corruption)
test-wal:
	@echo "Running WAL integration tests..."
	./scripts/test-wal.sh
