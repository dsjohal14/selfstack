# selfstack

Personal data brain: ingest → timeline → search → AI relay → automate.

## Quick Start

### Prerequisites
- **Go 1.22+** – [Install Go](https://go.dev/doc/install)
- **Docker & Docker Compose** – [Install Docker](https://docs.docker.com/get-docker/)
- **Make** – Usually pre-installed on macOS/Linux; Windows users can use WSL

### Running Locally

1. **Clone and install dependencies**:
   ```bash
   git clone https://github.com/dsjohal14/selfstack.git
   cd selfstack
   go mod download
   ```

2. **Start local services** (Postgres):
   ```bash
   docker compose -f ops/docker-compose.yml up -d
   ```

3. **Run migrations**:
   ```bash
   make migrate
   ```

4. **Start the API server**:
   ```bash
   make api
   ```
   The API will be available at `http://localhost:8080`

5. **Run tests**:
   ```bash
   go test ./... -v
   ```

### Development Commands
- `make api` – Start API server
- `make worker` – Start background worker
- `make fmt` – Format Go code
- `make tidy` – Tidy Go modules
- `make migrate` – Run database migrations
- `make test` – Run all tests

### Next Steps
- See [docs/contrib.md](docs/contrib.md) for coding standards
- Check [contracts/](contracts/) for shared data schemas
- Review architecture in [docs/](docs/)

## Layout
- cmd/ – binaries (api, worker, cli)
- internal/streamlite – connectors (files, pg_cdc, logs)
- internal/scope – storage & query (db, services, FTS, vectors)
- internal/relay – AI layer (summaries, tags, linking, rules)
- internal/libs – shared utils (accel, config, obs, jobs)
- migrations/ – SQL (Postgres + pgvector)
- contracts/ – JSON schemas for shared data contracts
- fe/ – Next.js dashboard
- ops/ – docker/compose/terraform/k8s
- docs/ – ADRs/design
