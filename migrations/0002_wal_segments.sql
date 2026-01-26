-- WAL Segments and State Tables
-- Tracks all WAL segments for durability and compaction

-- Segment status lifecycle: active -> sealed -> compacting -> archived
CREATE TABLE wal_segments (
    id              BIGSERIAL PRIMARY KEY,
    segment_id      BIGINT NOT NULL UNIQUE,
    filename        TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    record_count    INT NOT NULL DEFAULT 0,
    min_lsn         BIGINT,          -- First LSN in segment
    max_lsn         BIGINT,          -- Last LSN in segment
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sealed_at       TIMESTAMPTZ,
    checksum        TEXT,            -- CRC32 of entire segment (set on seal)
    CONSTRAINT valid_status CHECK (status IN ('active', 'sealed', 'compacting', 'archived'))
);

CREATE INDEX idx_segments_status ON wal_segments(status);
CREATE INDEX idx_segments_lsn ON wal_segments(min_lsn, max_lsn);
CREATE INDEX idx_segments_segment_id ON wal_segments(segment_id);

-- Singleton table for global WAL state
CREATE TABLE wal_state (
    id                  INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),  -- Singleton constraint
    current_segment_id  BIGINT NOT NULL DEFAULT 1,
    next_lsn            BIGINT NOT NULL DEFAULT 1,
    checkpoint_lsn      BIGINT NOT NULL DEFAULT 0,  -- Last LSN flushed to compacted storage
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert initial state row
INSERT INTO wal_state (id, current_segment_id, next_lsn, checkpoint_lsn)
VALUES (1, 1, 1, 0)
ON CONFLICT (id) DO NOTHING;
