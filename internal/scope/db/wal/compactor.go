package wal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CompactorConfig holds configuration for the compactor
type CompactorConfig struct {
	// MinSegmentsToCompact is the minimum number of sealed segments before compaction
	MinSegmentsToCompact int

	// MaxSegmentsPerCompaction limits how many segments are merged at once
	MaxSegmentsPerCompaction int

	// CompactionInterval is how often to check for compaction opportunities
	CompactionInterval time.Duration

	// TmpDir is the directory for temporary files during compaction
	TmpDir string
}

// DefaultCompactorConfig returns a reasonable default configuration
func DefaultCompactorConfig() CompactorConfig {
	return CompactorConfig{
		MinSegmentsToCompact:     2,
		MaxSegmentsPerCompaction: 10,
		CompactionInterval:       5 * time.Minute,
		TmpDir:                   "",
	}
}

// Compactor merges sealed WAL segments and removes tombstones
type Compactor struct {
	manifest   ManifestStore
	db         *pgxpool.Pool
	segmentDir string
	config     CompactorConfig

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewCompactor creates a new compactor
func NewCompactor(manifest ManifestStore, db *pgxpool.Pool, segmentDir string, config CompactorConfig) *Compactor {
	if config.TmpDir == "" {
		config.TmpDir = filepath.Join(segmentDir, ".tmp")
	}

	return &Compactor{
		manifest:   manifest,
		db:         db,
		segmentDir: segmentDir,
		config:     config,
	}
}

// Start begins the background compaction process
func (c *Compactor) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("compactor already running")
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.doneCh = make(chan struct{})
	c.mu.Unlock()

	// Create tmp directory
	if err := os.MkdirAll(c.config.TmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %w", err)
	}

	go c.runLoop(ctx)
	return nil
}

// Stop stops the background compaction process
func (c *Compactor) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	close(c.stopCh)
	<-c.doneCh

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

// runLoop is the main compaction loop
func (c *Compactor) runLoop(ctx context.Context) {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.config.CompactionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			if err := c.Compact(ctx); err != nil {
				// Log error but continue
				fmt.Printf("compaction error: %v\n", err)
			}
		}
	}
}

// Compact performs a single compaction run
func (c *Compactor) Compact(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get sealed segments
	segments, err := c.manifest.GetSealedSegments(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sealed segments: %w", err)
	}

	if len(segments) < c.config.MinSegmentsToCompact {
		return nil // Nothing to compact
	}

	// Sort by segment ID
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].SegmentID < segments[j].SegmentID
	})

	// Limit number of segments to compact
	if len(segments) > c.config.MaxSegmentsPerCompaction {
		segments = segments[:c.config.MaxSegmentsPerCompaction]
	}

	return c.compactSegments(ctx, segments)
}

// compactSegments merges the given segments into a new compacted segment
func (c *Compactor) compactSegments(ctx context.Context, segments []SegmentInfo) error {
	if len(segments) == 0 {
		return nil
	}

	// Mark segments as compacting
	for _, seg := range segments {
		if err := c.manifest.UpdateSegmentStatus(ctx, seg.SegmentID, SegmentStatusCompacting); err != nil {
			return fmt.Errorf("failed to mark segment %d as compacting: %w", seg.SegmentID, err)
		}
	}

	// Merge records
	records, tombstones, err := c.mergeRecords(segments)
	if err != nil {
		// Rollback status
		for _, seg := range segments {
			_ = c.manifest.UpdateSegmentStatus(ctx, seg.SegmentID, SegmentStatusSealed)
		}
		return fmt.Errorf("failed to merge records: %w", err)
	}

	// Filter out tombstoned records
	filteredRecords := make(map[string]*Record)
	for docID, rec := range records {
		if !tombstones[docID] {
			filteredRecords[docID] = rec
		}
	}

	if len(filteredRecords) == 0 {
		// All records were tombstoned, just archive the segments
		segmentIDs := make([]uint64, len(segments))
		for i, seg := range segments {
			segmentIDs[i] = seg.SegmentID
		}
		if err := c.manifest.ArchiveSegments(ctx, segmentIDs); err != nil {
			return fmt.Errorf("failed to archive segments: %w", err)
		}
		// Delete segment files
		for _, seg := range segments {
			_ = os.Remove(seg.Filename)
		}
		return nil
	}

	// Write merged segment to temp file
	tmpPath := filepath.Join(c.config.TmpDir, fmt.Sprintf("compact_%d.seg", time.Now().UnixNano()))
	writer, err := NewSegmentWriter(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp segment: %w", err)
	}

	// Sort records by LSN for consistent ordering
	sortedRecords := make([]*Record, 0, len(filteredRecords))
	for _, rec := range filteredRecords {
		sortedRecords = append(sortedRecords, rec)
	}
	sort.Slice(sortedRecords, func(i, j int) bool {
		return sortedRecords[i].LSN < sortedRecords[j].LSN
	})

	var minLSN, maxLSN uint64
	for i, rec := range sortedRecords {
		if err := writer.Write(rec); err != nil {
			_ = writer.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to write record: %w", err)
		}
		if i == 0 {
			minLSN = rec.LSN
		}
		maxLSN = rec.LSN
	}

	checksum, err := writer.Finalize()
	if err != nil {
		_ = writer.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize segment: %w", err)
	}

	sizeBytes := writer.Offset()
	_ = writer.Close()

	// Determine new segment ID - CRITICAL: must be higher than both:
	// 1. The highest compacted segment
	// 2. The current active segment (to prevent collision)
	newSegmentID := segments[len(segments)-1].SegmentID + 1

	// Check WAL state to get current active segment ID
	walState, err := c.manifest.GetWALState(ctx)
	if err == nil && walState != nil && walState.CurrentSegmentID >= newSegmentID {
		newSegmentID = walState.CurrentSegmentID + 1
	}

	// Also check for active segments in manifest
	activeSegs, err := c.manifest.GetSegmentsByStatus(ctx, SegmentStatusActive)
	if err == nil {
		for _, seg := range activeSegs {
			if seg.SegmentID >= newSegmentID {
				newSegmentID = seg.SegmentID + 1
			}
		}
	}

	// Atomic swap in transaction
	tx, err := c.db.Begin(ctx)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Archive old segments
	for _, seg := range segments {
		_, err := tx.Exec(ctx, "UPDATE wal_segments SET status = 'archived' WHERE segment_id = $1", seg.SegmentID)
		if err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failed to archive segment %d: %w", seg.SegmentID, err)
		}
	}

	// Move temp file to final location
	finalPath := filepath.Join(c.segmentDir, SegmentFilename(newSegmentID))
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to move compacted segment: %w", err)
	}

	// Register new segment
	_, err = tx.Exec(ctx, `
		INSERT INTO wal_segments (segment_id, filename, size_bytes, record_count, min_lsn, max_lsn, status, checksum, sealed_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'sealed', $7, NOW())
	`, newSegmentID, finalPath, sizeBytes, len(sortedRecords), minLSN, maxLSN, checksum)
	if err != nil {
		// Try to move file back or delete it
		_ = os.Remove(finalPath)
		return fmt.Errorf("failed to register compacted segment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		_ = os.Remove(finalPath)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Delete old segment files
	for _, seg := range segments {
		_ = os.Remove(seg.Filename)
	}

	return nil
}

// mergeRecords reads all records from segments, returning latest version of each document
func (c *Compactor) mergeRecords(segments []SegmentInfo) (map[string]*Record, map[string]bool, error) {
	records := make(map[string]*Record)  // DocID -> latest record
	tombstones := make(map[string]bool)  // DocID -> is deleted
	recordLSN := make(map[string]uint64) // DocID -> LSN of latest record

	for _, seg := range segments {
		// Verify checksum if available
		if seg.Checksum != nil {
			valid, err := VerifySegmentChecksum(seg.Filename, *seg.Checksum)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to verify segment %s: %w", seg.Filename, err)
			}
			if !valid {
				return nil, nil, fmt.Errorf("segment %s checksum mismatch", seg.Filename)
			}
		}

		iter, err := NewSegmentIterator(seg.Filename)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open segment %s: %w", seg.Filename, err)
		}

		for iter.Next() {
			rec := iter.Record()

			var docID string
			switch rec.Type {
			case RecordTypeInsert, RecordTypeUpdate:
				var err error
				docID, _, _, err = DecodeDocPayload(rec.Payload)
				if err != nil {
					_ = iter.Close()
					return nil, nil, fmt.Errorf("failed to decode payload: %w", err)
				}
			case RecordTypeDelete:
				var err error
				docID, err = DecodeDeletePayload(rec.Payload)
				if err != nil {
					_ = iter.Close()
					return nil, nil, fmt.Errorf("failed to decode delete payload: %w", err)
				}
			case RecordTypeCheckpoint:
				// Skip checkpoint records
				continue
			default:
				continue
			}

			// Only keep the record with the highest LSN for each document
			existingLSN, exists := recordLSN[docID]
			if !exists || rec.LSN > existingLSN {
				recordLSN[docID] = rec.LSN
				if rec.Type == RecordTypeDelete {
					tombstones[docID] = true
					delete(records, docID)
				} else {
					delete(tombstones, docID)
					// Make a copy of the record
					recCopy := *rec
					recCopy.Payload = make([]byte, len(rec.Payload))
					copy(recCopy.Payload, rec.Payload)
					records[docID] = &recCopy
				}
			}
		}

		if err := iter.Err(); err != nil {
			_ = iter.Close()
			return nil, nil, fmt.Errorf("error reading segment %s: %w", seg.Filename, err)
		}
		_ = iter.Close()
	}

	return records, tombstones, nil
}

// CompactOnce performs a single compaction without starting the background loop
func (c *Compactor) CompactOnce(ctx context.Context) error {
	return c.Compact(ctx)
}

// ForceCompact compacts all sealed segments regardless of minimum count
func (c *Compactor) ForceCompact(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	segments, err := c.manifest.GetSealedSegments(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sealed segments: %w", err)
	}

	if len(segments) < 2 {
		return nil // Need at least 2 segments
	}

	return c.compactSegments(ctx, segments)
}
