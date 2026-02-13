# Storage Documentation

## Overview

Selfstack uses **WAL (Write-Ahead Log) storage by default** for production-grade durability with crash recovery, compaction, and corruption detection.

## Storage Modes

| Mode | Command | Features |
|------|---------|----------|
| **Production** | `make api` | WAL + Postgres + Compaction |
| **Development** | `make api-dev` | WAL + in-memory manifest |
| **Legacy** | `make api-legacy` | Simple file storage |

## WAL Architecture

### File Layout

```
data/
└── wal/
    ├── wal_000000000001.seg   # Sealed segment
    ├── wal_000000000002.seg   # Sealed segment
    └── wal_000000000003.seg   # Active (being written)
```

### Record Format

```
┌─────────────────────────────────────────────────────────────┐
│ Magic (4B)  │ Type (1B) │ Flags (1B) │ Reserved (2B)        │
├─────────────────────────────────────────────────────────────┤
│ LSN (8B) - Log Sequence Number                              │
├─────────────────────────────────────────────────────────────┤
│ PayloadLen (4B)                                             │
├─────────────────────────────────────────────────────────────┤
│ HeaderCRC32 (4B)                                            │
├─────────────────────────────────────────────────────────────┤
│ Payload (variable)                                           │
├─────────────────────────────────────────────────────────────┤
│ PayloadCRC32 (4B)                                            │
└─────────────────────────────────────────────────────────────┘
```

**Record Types:**
- `0x01` INSERT - New document
- `0x02` UPDATE - Replace existing
- `0x03` DELETE - Tombstone
- `0x04` CHECKPOINT - Flushed position

### Postgres Manifest

When `DATABASE_URL` is set, segment metadata is tracked in Postgres:

```sql
-- Tracks all WAL segments
CREATE TABLE wal_segments (
    segment_id BIGINT NOT NULL UNIQUE,
    filename TEXT NOT NULL,
    status TEXT DEFAULT 'active',  -- active, sealed, compacting, archived
    checksum TEXT
);

-- Global WAL state
CREATE TABLE wal_state (
    current_segment_id BIGINT NOT NULL,
    next_lsn BIGINT NOT NULL DEFAULT 1,
    checkpoint_lsn BIGINT NOT NULL DEFAULT 0
);
```

**Segment Lifecycle:**
```
active → sealed → compacting → archived
```

## Features

### Crash Recovery

On startup, WALStore:
1. Scans all WAL segment files
2. Verifies checksums
3. Rebuilds in-memory index
4. Resumes from correct LSN

### Compaction

Background compaction (enabled by default with Postgres):
- Merges sealed segments
- Removes tombstoned documents
- Deduplicates by LSN (latest wins)
- Runs every 5 minutes

### Corruption Handling

- CRC32 checksums on header and payload
- Corrupt records are skipped during recovery
- Segment checksums verified before compaction

### Sync Policies

| Policy | Env Var | Durability | Performance |
|--------|---------|------------|-------------|
| Immediate | `WAL_SYNC_IMMEDIATE=true` | Maximum | ~1-5ms/write |
| Batched | `WAL_SYNC_IMMEDIATE=false` | High | <1ms/write |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | - | Postgres connection string |
| `WAL_DISABLED` | `false` | Use legacy file storage |
| `WAL_COMPACTION` | `true`* | Background compaction (*when Postgres set) |
| `WAL_SYNC_IMMEDIATE` | `true` | Fsync after every write |
| `DATA_DIR` | `./data` | Base data directory |

## Testing

Run the WAL integration test suite:

```bash
make test-wal
```

This tests:
- Ingesting 100 documents
- WAL file creation
- Postgres manifest tracking
- Crash recovery (kill -9)
- Corruption handling
- Segment rotation

## Performance

| Metric | Value |
|--------|-------|
| Write latency (immediate sync) | ~1-5ms |
| Write latency (batched) | <1ms |
| Recovery time | O(N) segments |
| Segment size | 64MB |

## Troubleshooting

### "WAL recovery failed"
- Check WAL directory permissions
- Verify no corrupted segments
- Check Postgres connection

### "Segment checksum mismatch"
- Segment file is corrupted
- Will be skipped during recovery
- Delete and let compaction rebuild

### "LSN rewind detected"
- Manifest state is stale
- Recovery uses max(manifest, files)
- Safe - handled automatically
