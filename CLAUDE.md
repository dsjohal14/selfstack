# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build, Test & Lint Commands

```bash
make api          # Start API server (http://localhost:8080)
make worker       # Start background worker
make test         # Run all tests (go test ./... -v -count=1)
make lint         # Run golangci-lint
make fmt          # Format code with gofmt
make tidy         # Clean up Go modules
make precommit    # Run all checks: fmt → tidy → lint → test (required before commits)
```

Run a single test:
```bash
go test -v -run TestFunctionName ./internal/path/to/package/...
```

Infrastructure:
```bash
docker compose -f ops/docker-compose.yml up -d  # Start PostgreSQL
```

## Architecture

Selfstack is a **personal data brain** that collects data from multiple sources, understands it using AI, and enables intelligent search and automation. Three-layer clean architecture:

1. **Streamlite** (`internal/streamlite/`) - Data ingestion adapters (file watchers, CDC, API polling)
   - Each connector should be self-contained with graceful error recovery
2. **Scope** (`internal/scope/`) - Storage & search plane
   - `db/` - Document storage with JSONL metadata + binary vectors
   - `search/` - Search facade (enables easy backend swapping)
3. **Relay** (`internal/relay/`) - AI "brain" layer
   - Deterministic SHA256-based 128-dim embeddings (placeholder for ML models)
   - Cosine similarity search, entity linking, LLM orchestration
   - All AI/ML logic belongs here

**Supporting layers:**
- `libs/accel/` - Performance seams ONLY (SIMD, Rust bindings) - NO business logic
- `libs/config/` - Configuration management
- `libs/obs/` - Logging, metrics (observability)
- `libs/jobs/` - Lightweight job queue runner

## Key Architectural Rules

- No cross-layer circular imports
- `main()` only in `cmd/` - keep it minimal (setup & routing only)
- `internal/` enforces encapsulation
- `libs/accel/` is ONLY for performance primitives, NOT for AI/embeddings

**What NOT to do:**
- Don't put embeddings in `accel/` (they belong in `relay/`)
- Don't put business logic in `cmd/api/main.go` (use handlers)

## API Development Pattern

1. Define contract in `/contracts/*.schema.json`
2. Create DTO in `internal/http/dto.go` matching contract
3. Implement handler in domain-specific file (e.g., `handlers_search.go`)
4. Add route in `cmd/api/main.go`
5. Write tests in `handlers_test.go`
6. Document in `docs/api.md`

**Contract compliance:**
- All DTOs must match `/contracts/*.schema.json` specs
- Required fields for docs: `id`, `source`, `title`, `text`, `created_at`
- Use `time.Time` for timestamps (not strings)
- Use `map[string]string` for metadata

## Storage Format

- `data/metadata.jsonl` - One JSON document per line
- `data/vectors.bin` - Binary matrix (header + float32 little-endian vectors, 128-dim)
- Search is brute-force O(N×D) cosine similarity (add ANN index for >100k docs)

## Code Style

- Wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use structured logging with zerolog
- Table-driven tests with co-located `*_test.go` files
- Conventional commits: `feat(scope): add events repo`, `fix(relay): embedding normalization`

## Testing Requirements

- **Unit tests**: Every package needs `*_test.go`
- **Integration tests**: Full pipeline tests (ingest → search → run)
- **Black-box tests**: Test via HTTP layer, not internal packages
- **Pre-commit**: `make precommit` must pass

## File Naming Conventions

- Commands: `cmd/{name}/main.go`
- Handlers: `internal/http/handlers_{domain}.go`
- Tests: `{package}_test.go` (co-located)
- DTOs: `internal/http/dto.go` or `dto_{domain}.go` for large APIs

## Questions Before Adding Code

1. **Which layer?** Is this ingestion (streamlite), storage (scope), or AI (relay)?
2. **Handler organization?** If HTTP, does it need a separate handler file?
3. **Contract match?** Does the DTO match `/contracts/` schema?
4. **Tests?** Have I added tests for this new code?

## API Endpoints

- `GET /health` - Health check + document count
- `POST /ingest` - Document ingestion with auto-embedding
- `POST /search` - Semantic search by cosine similarity
- `POST /run` - AI agent query with citations
