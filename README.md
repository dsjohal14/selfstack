# Selfstack

**Your personal data brain** - Collects, understands, and automates your data privately.

## Quick Start

```bash
# 1. Clone and setup
git clone https://github.com/dsjohal14/selfstack.git
cd selfstack
go mod download

# 2. Start Postgres and run migrations
make db-up

# 3. Start the API (production mode: WAL + Postgres + Compaction)
make api
# → Running at http://localhost:8080

# 4. Ingest a document
curl -X POST http://localhost:8080/ingest \
  -H "Content-Type: application/json" \
  -d '{"id": "doc1", "source": "test", "title": "Hello", "text": "Selfstack is a personal data brain"}'

# 5. Search
curl -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query": "data brain", "limit": 5}'
```

## Features

- **Write-Ahead Log (WAL)** - Durable writes with crash recovery
- **Postgres Manifest** - Tracks segments for reliable recovery
- **Background Compaction** - Merges segments, removes tombstones
- **CRC32 Checksums** - Detects corruption automatically
- **Semantic Search** - Cosine similarity over embeddings

## Make Commands

```bash
# Production (WAL + Postgres + Compaction)
make api           # Start with full durability

# Development (WAL, no Postgres)
make api-dev       # Start with in-memory manifest

# Database
make db-up         # Start Postgres + run migrations
make db-down       # Stop Postgres

# Testing
make test          # Run unit tests
make test-wal      # Run WAL integration tests (100 events, crash recovery, etc.)
make precommit     # Format + lint + test
```

## Storage Modes

| Mode | Command | Durability | Compaction |
|------|---------|------------|------------|
| Production | `make api` | WAL + Postgres | Yes |
| Development | `make api-dev` | WAL + in-memory | No |
| Legacy | `make api-legacy` | File-based | No |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | - | Postgres connection (enables manifest + compaction) |
| `WAL_DISABLED` | `false` | Use legacy file storage |
| `WAL_COMPACTION` | `true`* | Enable background compaction (*when Postgres is set) |
| `WAL_SYNC_IMMEDIATE` | `true` | Sync after every write |
| `DATA_DIR` | `./data` | Data directory |

## Architecture

```
selfstack/
├── cmd/api/           # HTTP server
├── internal/
│   ├── http/          # Handlers & DTOs
│   ├── scope/db/      # Storage (WAL + compaction)
│   │   └── wal/       # WAL implementation
│   ├── relay/         # AI layer (embeddings)
│   └── libs/          # Config, logging
├── migrations/        # SQL schemas
└── scripts/           # Test scripts
```

## API Endpoints

- `GET /health` - Health check + document count
- `POST /ingest` - Ingest document with auto-embedding
- `POST /search` - Semantic search
- `POST /run` - AI agent with citations

## Documentation

- [API Reference](docs/api.md)
- [Storage & WAL](docs/storage.md)
- [Contributing](docs/contrib.md)

## License

[Add license]
