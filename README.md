# Selfstack

**Your personal data brain** - Collects, understands, and automates your data privately.

Selfstack brings together notes, files, emails, logs, metrics, and database changes into a searchable timeline, then uses AI to answer questions and automate tasks—all while keeping your data under your control.

## What It Does

- 🔄 **Pulls data in** - Files, databases, logs, APIs, change events
- 📊 **Keeps a timeline** - Time-ordered record you can search and replay
- 🧠 **Understands & links** - AI summarizes, tags, and connects related items
- 💬 **Answers questions** - Plain-English queries with source citations
- ⚡ **Automates tasks** - Set rules like "if X happens, do Y"
- 🔒 **Private by default** - Your data stays with you

## Who It's For

- **Engineers/Builders** - Watch code, infra, and data changes with smart alerts
- **Indie Makers** - Personal knowledge system that automates boring checks
- **Privacy-focused** - Google-level search across your own data, but hackable and private

## Example Use Cases

```
You push code → Selfstack notices logs & DB changes → summarizes risks → links to commits

Sales sheet updates → spots regional drop → auto-posts summary to Slack

You ask: "Show payment errors after 6pm post-deploy" → timeline with source links

Drop a PDF spec → extracts key decisions → creates follow-up tasks
```

## Quick Start

### Prerequisites
- **Go 1.22+** - [Install Go](https://go.dev/doc/install)
- **Docker & Docker Compose** - [Install Docker](https://docs.docker.com/get-docker/)
- **Make** - Usually pre-installed (macOS/Linux)

### Get Running in 5 Minutes

```bash
# 1. Clone and setup
git clone https://github.com/dsjohal14/selfstack.git
cd selfstack
go mod download

# 2. Start local services (Postgres)
docker compose -f ops/docker-compose.yml up -d

# 3. Start the API server
make api
# → Running at http://localhost:8080

# 4. Try it out!
# Ingest a document
curl -X POST http://localhost:8080/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "id": "doc1",
    "source": "test",
    "title": "Getting Started",
    "text": "Selfstack is a personal data brain for engineers"
  }'

# Search for it
curl -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{"query": "data brain", "limit": 5}'

# Ask AI agent
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{"query": "What is Selfstack?"}'
```

### Development Commands

```bash
make api        # Start API server
make worker     # Start background worker
make fmt        # Format code
make lint       # Run linter  
make test       # Run tests
make precommit  # Run all checks (fmt + lint + test)
```

## Architecture

Selfstack is built with three core layers:

### 🔌 Streamlite (Data Ingestion)
Connectors that listen to sources and stream changes in real-time
- File watchers, database CDC, log streaming, API polling

### 💾 Scope (Storage & Search)
Fast timeline database with full-text and semantic search
- Document storage with embeddings
- PostgreSQL + pgvector (moving to Typesense/Elastic)

### 🧠 Relay (AI "Brain")
Intelligence layer for understanding and automation
- Embeddings (deterministic for MVP, ML models for V1)
- Summarization, tagging, entity linking
- Rules engine for automation
- LLM orchestration

## API Endpoints

- `GET /health` - Health check and doc count
- `POST /ingest` - Ingest documents with metadata
- `POST /search` - Semantic search with similarity scores
- `POST /run` - AI agent queries with citations

## Current Status: MVP ✅

**What's Working:**
- ✅ Document ingestion with contract validation
- ✅ Binary vector storage (128-dim embeddings)
- ✅ Semantic search via cosine similarity
- ✅ AI agent with source citations
- ✅ Full API with comprehensive tests
- ✅ Clean architecture (streamlite → scope → relay)

**Coming in V1:**
- More connectors (Gmail, GitHub, Postgres CDC, S3, Kafka)
- Real ML embeddings (OpenAI, sentence-transformers)
- Dashboards and saved investigations
- Rules engine ("if error spikes, file ticket")
- Next.js frontend

## Documentation

- 📖 [API Documentation](docs/api.md) - Complete API reference with examples
- 🏗️ [Storage Format](docs/storage.md) - Vector storage & embedding algorithm
- 👥 [Contributing](docs/contrib.md) - Coding standards and development workflow
- 📋 [Contracts](contracts/) - JSON schemas for data types
- ✅ [MVP Complete](docs/mvp-complete.md) - What's been built

## Project Structure

```
selfstack/
├── cmd/                # Executables (api, worker, cli)
├── internal/
│   ├── http/          # HTTP handlers & DTOs
│   ├── streamlite/    # Data ingestion connectors
│   ├── scope/         # Storage & search (db, search)
│   ├── relay/         # AI/automation layer
│   └── libs/          # Shared utilities
├── contracts/         # JSON schemas for shared contracts
├── migrations/        # SQL migrations (Postgres + pgvector)
├── docs/             # Architecture & API documentation
└── ops/              # Docker/compose/k8s/terraform
```

## Tech Stack

- **Backend**: Go 1.22+
- **Database**: PostgreSQL 16 with pgvector
- **HTTP**: Chi router
- **Logging**: Zerolog (structured logging)
- **AI**: Deterministic embeddings (MVP), ML models (V1)
- **Frontend**: Next.js (coming in V1)

## Contributing

We follow clean architecture principles:

1. **Streamlite** = Data Ingestion
2. **Scope** = Storage & Query
3. **Relay** = AI & Automation

Before submitting:
```bash
make precommit  # Format, lint, and test
```

See [CONTRIBUTING.md](docs/contrib.md) for detailed guidelines.

## Performance

**Current (MVP)**:
- ~100ms for 100k docs (brute-force search)
- 512 bytes per document (vector storage)
- Single-node, file-based storage

**Planned (V1)**:
- ANN search for 1M+ docs (HNSW/IVF)
- SIMD optimizations in `libs/accel`
- Distributed storage option

## Privacy & Security

- **Private by default** - Your data never leaves your infrastructure
- **No telemetry** - Zero data collection
- **Hackable** - Open architecture, easy to extend
- **Local-first** - Works offline, sync optional

## License

[Add your license here]

## Support & Community

- 📧 Issues: [GitHub Issues](https://github.com/dsjohal14/selfstack/issues)
- 💬 Discussions: [GitHub Discussions](https://github.com/dsjohal14/selfstack/discussions)
- 🐦 Updates: [Twitter/X](https://twitter.com/yourhandle)

## Acknowledgments

Built with inspiration from:
- Personal Knowledge Management (Logseq, Obsidian)
- Observability Tools (Elastic Stack)
- AI-powered search (Khoj, Perplexity)

---

**Status**: MVP Complete ✅ | **Version**: 0.1.0 | **Go**: 1.22+
