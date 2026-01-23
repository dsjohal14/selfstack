# Selfstack API Documentation

## Overview

The Selfstack API provides four main endpoints for document ingestion, search, health checks, and AI-powered query answering.

**Base URL**: `http://localhost:8080` (configurable via `API_HOST` and `API_PORT`)

## Endpoints

### 1. Health Check

**GET** `/health`

Check API server status and document count.

**Response**:
```json
{
  "status": "healthy",
  "doc_count": 42
}
```

**Status Codes**:
- `200 OK` - Service is healthy

---

### 2. Ingest Document

**POST** `/ingest`

Ingest a document into the knowledge base with deterministic embedding generation.

**Request**:
```json
{
  "id": "doc-123",
  "text": "Your document text here",
  "metadata": {
    "source": "notion",
    "author": "jane@example.com"
  }
}
```

**Fields**:
- `id` (string, required) - Unique document identifier
- `text` (string, required) - Document content to embed and store
- `metadata` (object, optional) - Key-value metadata

**Response**:
```json
{
  "id": "doc-123",
  "success": true,
  "message": "document ingested successfully"
}
```

**Status Codes**:
- `200 OK` - Document ingested successfully
- `400 Bad Request` - Invalid request (missing id or text)
- `500 Internal Server Error` - Storage failure

**Notes**:
- Embeddings are generated deterministically using SHA256-based pseudo-random vectors
- Documents with duplicate IDs will be updated in place
- Changes are immediately persisted to disk

---

### 3. Search Documents

**POST** `/search`

Search for documents semantically similar to a query.

**Request**:
```json
{
  "query": "machine learning algorithms",
  "limit": 10
}
```

**Fields**:
- `query` (string, required) - Search query text
- `limit` (integer, optional) - Maximum results (default: 10)

**Response**:
```json
{
  "results": [
    {
      "doc_id": "doc-123",
      "score": 0.87,
      "text": "Full document text..."
    },
    {
      "doc_id": "doc-456",
      "score": 0.65,
      "text": "Another relevant document..."
    }
  ],
  "count": 2
}
```

**Result Fields**:
- `doc_id` - Document identifier
- `score` - Cosine similarity score (0-1, higher = more similar)
- `text` - Full document text

**Status Codes**:
- `200 OK` - Search completed
- `400 Bad Request` - Missing query

**Notes**:
- Uses cosine similarity over 128-dimensional embeddings
- Results sorted by score descending
- Empty results if no documents match

---

### 4. Run Agent Query

**POST** `/run`

Execute an AI agent query that searches documents and composes an answer with citations.

**Request**:
```json
{
  "query": "What are the benefits of microservices?"
}
```

**Fields**:
- `query` (string, required) - Natural language question

**Response**:
```json
{
  "answer": "Based on 3 documents:\n\n1. [doc-123] (score: 0.92) Microservices enable independent deployment...\n2. [doc-456] (score: 0.78) Benefits include scalability and fault isolation...\n3. [doc-789] (score: 0.65) Teams can work autonomously on different services...",
  "citations": [
    {
      "doc_id": "doc-123",
      "score": 0.92,
      "text": "Microservices enable independent deployment..."
    },
    {
      "doc_id": "doc-456",
      "score": 0.78,
      "text": "Benefits include scalability and fault isolation..."
    },
    {
      "doc_id": "doc-789",
      "score": 0.65,
      "text": "Teams can work autonomously on different services..."
    }
  ]
}
```

**Status Codes**:
- `200 OK` - Query processed
- `400 Bad Request` - Missing query

**Notes**:
- Returns top 3 most relevant documents as citations
- Answer is composed from retrieved documents
- Citations include full text and similarity scores

---

## Error Responses

All errors follow this format:

```json
{
  "error": "description of what went wrong"
}
```

Common error status codes:
- `400 Bad Request` - Invalid input
- `500 Internal Server Error` - Server-side failure

---

## Configuration

Environment variables:
- `API_HOST` - Server host (default: `0.0.0.0`)
- `API_PORT` - Server port (default: `8080`)
- `DATA_DIR` - Data storage directory (default: `./data`)
- `LOG_LEVEL` - Logging level: `debug`, `info`, `warn`, `error` (default: `info`)

---

## Example Usage

### Ingest documents
```bash
curl -X POST http://localhost:8080/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "id": "guide-1",
    "text": "Kubernetes is a container orchestration platform",
    "metadata": {"source": "docs"}
  }'
```

### Search
```bash
curl -X POST http://localhost:8080/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "container orchestration",
    "limit": 5
  }'
```

### Run agent
```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is Kubernetes used for?"
  }'
```

---

## Rate Limits

Currently no rate limits enforced (MVP).

## Authentication

Currently no authentication required (MVP). Add API keys or OAuth in production.

