-- Add segment_type column to distinguish WAL segments from compacted segments
-- This allows WAL and compacted segments to have overlapping IDs without collision

-- Add segment_type column with default 'wal' for existing rows
ALTER TABLE wal_segments ADD COLUMN IF NOT EXISTS segment_type TEXT NOT NULL DEFAULT 'wal';

-- Add constraint for valid segment types
ALTER TABLE wal_segments DROP CONSTRAINT IF EXISTS valid_segment_type;
ALTER TABLE wal_segments ADD CONSTRAINT valid_segment_type CHECK (segment_type IN ('wal', 'cmp'));

-- Drop the old unique constraint on segment_id alone
ALTER TABLE wal_segments DROP CONSTRAINT IF EXISTS wal_segments_segment_id_key;

-- Add composite unique constraint (segment_type, segment_id)
-- This allows both wal_5 and cmp_5 to exist without collision
ALTER TABLE wal_segments DROP CONSTRAINT IF EXISTS unique_segment_type_id;
ALTER TABLE wal_segments ADD CONSTRAINT unique_segment_type_id UNIQUE (segment_type, segment_id);

-- Update index for the new composite key
DROP INDEX IF EXISTS idx_segments_segment_id;
DROP INDEX IF EXISTS idx_segments_type_id;
CREATE INDEX idx_segments_type_id ON wal_segments(segment_type, segment_id);
