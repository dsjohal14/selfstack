# Storage Format Documentation

## Overview

Selfstack uses a simple on-disk storage format optimized for small to medium datasets (MVP target: <100k documents).

**Storage Location**: Configured via `DATA_DIR` environment variable (default: `./data`)

---

## File Layout

```
./data/
├── metadata.jsonl    # Document metadata (one JSON per line)
└── vectors.bin       # Binary embedding matrix
```

### metadata.jsonl

**Format**: JSON Lines (JSONL) - one JSON object per line

**Schema**:
```json
{"id":"doc-123","text":"document content","metadata":{"key":"value"}}
```

**Fields**:
- `id` (string) - Unique document identifier
- `text` (string) - Full document text
- `metadata` (object, optional) - User-provided key-value pairs

**Example**:
```
{"id":"doc1","text":"Python is great","metadata":{"lang":"en"}}
{"id":"doc2","text":"Go is fast","metadata":{"lang":"en"}}
{"id":"doc3","text":"Rust is safe","metadata":{"lang":"en"}}
```

**Properties**:
- Human-readable (valid JSON)
- Append-friendly
- Line-by-line processing
- Easy to inspect/debug

---

### vectors.bin

**Format**: Binary file with header + float32 matrix

**Structure**:
```
[Header: 8 bytes]
  - num_docs: uint32 (little-endian)
  - dimension: uint32 (little-endian)

[Vectors: num_docs × dimension × 4 bytes]
  - Each vector: dimension float32 values (little-endian)
  - Vectors stored in row-major order
```

**Constants**:
- `dimension` = 128 (fixed in MVP)
- `num_docs` = number of documents
- Float type: IEEE 754 32-bit (single precision)

**Example** (for 2 documents, dim=128):
```
Byte offset | Content
0-3         | num_docs = 2 (uint32)
4-7         | dimension = 128 (uint32)
8-519       | Vector 1: 128 float32 values (512 bytes)
520-1031    | Vector 2: 128 float32 values (512 bytes)
```

**Properties**:
- Space-efficient (4 bytes per dimension)
- Fast memory mapping (future optimization)
- CPU cache-friendly for scanning
- Deterministic byte layout

---

## Embedding Algorithm

### Deterministic Embeddings v1

**Purpose**: Generate reproducible fixed-dimension vectors from text without ML models.

**Algorithm**:
```
Input: text (string)
Output: embedding (128-dimensional unit vector)

1. hash = SHA256(text)
2. For i = 0 to 127:
     byte_offset = (i × 4) mod 32
     uint_val = read_uint32(hash[byte_offset:byte_offset+4])
     float_val = (uint_val / MAX_UINT32) × 2 - 1   // Map to [-1, 1]
     embedding[i] = float_val
3. Normalize embedding to unit length (L2 norm = 1)
```

**Properties**:
- **Deterministic**: Same text → same embedding, always
- **Fast**: SHA256 + simple math, no model inference
- **Reproducible**: Same result across machines, OS, compiler
- **Unit vectors**: Enables cosine similarity via dot product
- **NOT semantic**: Does not capture meaning (will be replaced with real embeddings)

**Example**:
```go
embedding := DeterministicEmbed("hello world")
// Always produces the same 128-dimensional vector
```

**Limitations**:
- No semantic understanding
- Similar texts may have low similarity
- Only useful for exact match and testing
- **Replace with sentence-transformers or OpenAI embeddings in production**

---

## Search Algorithm

### Cosine Similarity v0

**Method**: Brute-force scan over all vectors

**Formula**:
```
similarity(query, doc) = dot_product(query, doc)
```

Since all vectors are normalized to unit length:
```
dot_product(a, b) = cosine(angle between a and b)
```

**Range**: [-1, 1]
- `1.0` = identical vectors
- `0.0` = orthogonal (unrelated)
- `-1.0` = opposite vectors

**Algorithm**:
```
1. Embed query → query_vector
2. For each document:
     score = dot_product(query_vector, doc.embedding)
3. Sort documents by score descending
4. Return top K results
```

**Complexity**:
- Time: O(N × D) where N = num docs, D = dimension
- Space: O(N) for scores array

**MVP Limits**:
- Max corpus size: ~100k documents (10MB vectors)
- Search latency: <100ms for 100k docs on modern CPU
- No indexing (HNSW, IVF, etc.) - future optimization

---

## File Operations

### Write Flow
1. In-memory document list maintained
2. On `Flush()`:
   - Write `metadata.jsonl` (overwrite)
   - Write `vectors.bin` (overwrite)
3. Atomic writes (create temp file, rename)

### Read Flow
1. On startup, load both files:
   - Parse JSONL → documents
   - Read binary header → validate
   - Read vectors → attach to documents
2. Build in-memory index

### Update Flow
- Find document by ID
- Update in-memory
- Mark as modified
- Next `Flush()` writes to disk

---

## Versioning & Migration

**Current Version**: v1 (MVP)

**Breaking Changes** (require migration):
- Changing embedding dimension
- Changing float precision
- Changing endianness

**Future Improvements** (non-breaking):
- Add file format version header
- Add checksums for corruption detection
- Add compression (zstd)
- Add incremental updates (append-only vectors)

---

## Data Integrity

**Current Safeguards**:
- Metadata/vector count validation on load
- Dimension validation
- Atomic writes (temp + rename)

**Not Yet Implemented** (add in prod):
- File locking (concurrent writes)
- Checksums (corruption detection)
- Backup/replication
- Write-ahead log (crash recovery)

---

## Performance Characteristics

### Storage
- Metadata: ~100 bytes/doc (depends on text length)
- Vectors: 512 bytes/doc (128 × 4 bytes)
- **Total**: ~600 bytes/doc average

**Example**:
- 1k docs: ~600 KB
- 10k docs: ~6 MB
- 100k docs: ~60 MB

### Search Latency (single-threaded)
- 1k docs: <1ms
- 10k docs: ~10ms
- 100k docs: ~100ms

**Future Optimization**:
- SIMD vectorization (4-8x speedup)
- Multi-threading (linear speedup)
- Approximate search (ANN) for >1M docs

---

## Troubleshooting

### "Vector count mismatch"
- Cause: `metadata.jsonl` and `vectors.bin` out of sync
- Fix: Delete both files and re-ingest

### "Dimension mismatch"
- Cause: Code changed embedding dimension
- Fix: Delete storage, re-ingest with new dimension

### "File not found"
- Cause: First run, no data yet
- Fix: Normal - files created on first ingest

### Large file sizes
- Cause: Many documents or long texts
- Fix: Expected - monitor disk space

---

## Migration Guide (Future)

When upgrading embedding dimension or algorithm:

1. **Backup** current `./data` directory
2. **Export** metadata only:
   ```bash
   cp ./data/metadata.jsonl ./backup/metadata.jsonl
   ```
3. **Clear** vectors:
   ```bash
   rm ./data/vectors.bin
   ```
4. **Re-ingest** from metadata backup with new embedding function
5. **Verify** document count matches
6. **Delete** backup after validation

---

## Lock Down (Per DoD)

**CPU/Float Types**:
- CPU: x86_64 or ARM64 (both little-endian)
- Float: IEEE 754 single-precision (32-bit)
- Byte order: Little-endian
- Go version: 1.22+

**Do NOT Mix**:
- Different embedding dimensions
- Different float precisions
- Different byte orders (big/little endian)

**Validation** on load:
- Check dimension matches constant
- Check file sizes are consistent
- Fail loudly if mismatch detected

