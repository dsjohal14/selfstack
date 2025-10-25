# Shared Contracts

This directory contains JSON Schema definitions for all data types that flow through Selfstack. These schemas serve as the contract between components (streamlite, scope, relay) and ensure consistent data structures.

## Schema Version

**Current Version**: `v1.0.0` (2025-10-25)

All schemas follow [JSON Schema Draft 2020-12](https://json-schema.org/draft/2020-12/schema).

## Schemas

### 1. Doc (`doc.schema.json`)

**Purpose**: Represents a complete document ingested into Selfstack (e.g., note, file, email, log).

**Required Fields**:
- `id` (uuid) – Unique document identifier
- `source` (string) – Origin identifier (e.g., "notion", "filesystem:/docs")
- `title` (string) – Document title or subject
- `created_at` (date-time) – When document was created

**Optional Fields**:
- `text` (string) – Full document content
- `metadata` (object) – Source-specific metadata (flexible schema)

**Example**:
```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "source": "notion",
  "title": "Q4 Planning Notes",
  "text": "Detailed planning notes for Q4 2025...",
  "metadata": {
    "author": "jane@example.com",
    "tags": ["planning", "q4"]
  },
  "created_at": "2025-10-25T10:00:00Z"
}
```

---

### 2. Chunk (`chunk.schema.json`)

**Purpose**: A document split into smaller, semantically meaningful pieces for vector search and retrieval.

**Required Fields**:
- `doc_id` (uuid) – Parent document reference
- `index` (integer ≥0) – Chunk position in document
- `text` (string) – Chunk content

**Optional Fields**:
- `embedding` (array of numbers) – Vector representation for semantic search
- `metadata` (object) – Additional context (e.g., heading, section)

**Example**:
```json
{
  "doc_id": "123e4567-e89b-12d3-a456-426614174000",
  "index": 0,
  "text": "# Introduction\n\nOur Q4 goals focus on three pillars...",
  "embedding": [0.123, -0.456, 0.789, ...],
  "metadata": {
    "heading": "Introduction",
    "token_count": 42
  }
}
```

---

### 3. ChangeEvent (`change_event.schema.json`)

**Purpose**: Core event in the Selfstack timeline – represents any atomic change, log entry, or observation.

**Required Fields**:
- `id` (uuid) – Unique event identifier
- `source_id` (uuid) – Source/connector that produced this event
- `ts_observed` (date-time) – When Selfstack observed this event
- `subject` (string) – Brief summary or title

**Optional Fields**:
- `ts_origin` (date-time) – Original timestamp (if different from observed)
- `level` (string) – Severity/importance (e.g., "info", "warning", "error")
- `topic` (string) – Category or namespace (e.g., "db.migrations", "api.errors")
- `body_text` (string) – Full event details
- `labels` (array of strings) – Tags for filtering
- `hash` (string) – Content-based deduplication key
- `metadata` (object) – Additional structured data

**Example**:
```json
{
  "id": "223e4567-e89b-12d3-a456-426614174001",
  "source_id": "333e4567-e89b-12d3-a456-426614174002",
  "ts_observed": "2025-10-25T14:23:45Z",
  "ts_origin": "2025-10-25T14:23:43Z",
  "level": "error",
  "topic": "api.payments",
  "subject": "Payment processing timeout",
  "body_text": "Payment ID 12345 failed after 30s timeout connecting to Stripe API",
  "labels": ["payments", "timeout", "stripe"],
  "hash": "sha256:abc123...",
  "metadata": {
    "payment_id": "12345",
    "amount": 49.99,
    "customer_id": "cus_ABC"
  }
}
```

---

### 4. MetricPoint (`metric_point.schema.json`)

**Purpose**: Time-series data point for tracking system or business metrics.

**Required Fields**:
- `ts` (date-time) – Timestamp of measurement
- `name` (string) – Metric name (e.g., "api.response_time", "sales.total")
- `value` (number) – Metric value

**Optional Fields**:
- `labels` (object) – Key-value pairs for dimensions (e.g., `{"region": "us-east", "env": "prod"}`)

**Example**:
```json
{
  "ts": "2025-10-25T14:30:00Z",
  "name": "api.response_time_ms",
  "value": 245.3,
  "labels": {
    "endpoint": "/api/search",
    "status": "200",
    "region": "us-west"
  }
}
```

---

### 5. TraceStep (`trace_step.schema.json`)

**Purpose**: Represents one step in the data processing pipeline – used for observability and debugging.

**Required Fields**:
- `ts` (date-time) – When step occurred
- `kind` (enum) – Step type: `ingest`, `normalize`, `index`, `rule`, `llm`, `action`
- `message` (string) – Human-readable description

**Optional Fields**:
- `attrs` (object) – Additional context (doc_id, duration, error, etc.)

**Example**:
```json
{
  "ts": "2025-10-25T14:35:22Z",
  "kind": "llm",
  "message": "Generated summary for document",
  "attrs": {
    "doc_id": "123e4567-e89b-12d3-a456-426614174000",
    "model": "gpt-4",
    "tokens": 1523,
    "duration_ms": 2340
  }
}
```

---

## Validation

To validate JSON against these schemas in Go, use a library like:
- [xeipuuv/gojsonschema](https://github.com/xeipuuv/gojsonschema)
- [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema)

Example validation:
```go
schema, _ := jsonschema.Compile("contracts/doc.schema.json")
err := schema.Validate(strings.NewReader(jsonData))
```

## Versioning Strategy

- Schemas are versioned semantically (MAJOR.MINOR.PATCH)
- Breaking changes increment MAJOR (e.g., removing required field)
- Backward-compatible additions increment MINOR (e.g., new optional field)
- Documentation/fixes increment PATCH
- Schema `$id` includes version when needed

## Usage in Code

Each schema maps to a Go struct in the appropriate package:
- `Doc` & `Chunk` → `internal/scope/db/models.go`
- `ChangeEvent` → `internal/streamlite/types.go`
- `MetricPoint` → `internal/obs/metrics.go`
- `TraceStep` → `internal/obs/trace.go`

These contracts ensure all components speak the same language.

