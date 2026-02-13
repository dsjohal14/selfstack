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

	// Get sealed WAL segments only (not compacted segments)
	segments, err := c.manifest.GetSealedWALSegments(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sealed WAL segments: %w", err)
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

	// Helper to rollback segments to sealed status on any error
	// Uses background context with timeout to ensure rollback completes even if
	// the parent context is canceled (e.g., during shutdown)
	rollbackToSealed := func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, seg := range segments {
			_ = c.manifest.UpdateSegmentStatus(rollbackCtx, seg.SegmentID, SegmentStatusSealed)
		}
	}

	// Mark segments as compacting
	for _, seg := range segments {
		if err := c.manifest.UpdateSegmentStatus(ctx, seg.SegmentID, SegmentStatusCompacting); err != nil {
			// Rollback any already marked segments
			rollbackToSealed()
			return fmt.Errorf("failed to mark segment %d as compacting: %w", seg.SegmentID, err)
		}
	}

	// Merge records - returns live records and tombstone records separately
	records, tombstoneRecords, err := c.mergeRecords(segments)
	if err != nil {
		rollbackToSealed()
		return fmt.Errorf("failed to merge records: %w", err)
	}

	// IMPORTANT: We must preserve tombstones in the compacted output!
	// If we drop them, deleted documents can reappear when they exist in
	// older compacted segments (the tombstone is the only thing masking
	// the old INSERT during recovery).
	//
	// Merge live records and tombstones into a single map for writing
	allRecords := make(map[string]*Record)
	for docID, rec := range records {
		allRecords[docID] = rec
	}
	for docID, rec := range tombstoneRecords {
		allRecords[docID] = rec
	}

	if len(allRecords) == 0 {
		// No records at all, just archive the segments
		segmentIDs := make([]uint64, len(segments))
		for i, seg := range segments {
			segmentIDs[i] = seg.SegmentID
		}
		if err := c.manifest.ArchiveSegments(ctx, segmentIDs); err != nil {
			rollbackToSealed()
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
		rollbackToSealed()
		return fmt.Errorf("failed to create temp segment: %w", err)
	}

	// Sort records by LSN for consistent ordering
	sortedRecords := make([]*Record, 0, len(allRecords))
	for _, rec := range allRecords {
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
			rollbackToSealed()
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
		rollbackToSealed()
		return fmt.Errorf("failed to finalize segment: %w", err)
	}

	sizeBytes := writer.Offset()
	_ = writer.Close()

	// Determine new segment ID for the compacted segment
	// Compacted segments use a separate filename namespace (cmp_) to avoid
	// ID collisions with the live WAL writer during rotation
	newSegmentID := segments[len(segments)-1].SegmentID + 1

	// Atomic swap in transaction
	tx, err := c.db.Begin(ctx)
	if err != nil {
		_ = os.Remove(tmpPath)
		rollbackToSealed()
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Helper to cleanup on transaction errors - MUST rollback tx first to release
	// row locks before rollbackToSealed() tries to update on a separate connection
	// Uses background context to ensure rollback completes even if ctx is canceled
	cleanupTxError := func(filePath string) {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
		if filePath != "" {
			_ = os.Remove(filePath)
		}
		rollbackToSealed()
	}

	// Archive old WAL segments in transaction (will be committed atomically)
	for _, seg := range segments {
		_, err := tx.Exec(ctx, "UPDATE wal_segments SET status = 'archived' WHERE segment_id = $1 AND segment_type = 'wal'", seg.SegmentID)
		if err != nil {
			cleanupTxError(tmpPath)
			return fmt.Errorf("failed to archive WAL segment %d: %w", seg.SegmentID, err)
		}
	}

	// Move temp file to final location (use compacted segment namespace)
	finalPath := filepath.Join(c.segmentDir, CompactedSegmentFilename(newSegmentID))
	if err := os.Rename(tmpPath, finalPath); err != nil {
		cleanupTxError(tmpPath)
		return fmt.Errorf("failed to move compacted segment: %w", err)
	}

	// Register new compacted segment (segment_type='cmp')
	_, err = tx.Exec(ctx, `
		INSERT INTO wal_segments (segment_id, segment_type, filename, size_bytes, record_count, min_lsn, max_lsn, status, checksum, sealed_at, created_at)
		VALUES ($1, 'cmp', $2, $3, $4, $5, $6, 'sealed', $7, NOW(), NOW())
	`, newSegmentID, finalPath, sizeBytes, len(sortedRecords), minLSN, maxLSN, checksum)
	if err != nil {
		cleanupTxError(finalPath)
		return fmt.Errorf("failed to register compacted segment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		// Commit failed - tx already rolled back by driver, just cleanup
		_ = os.Remove(finalPath)
		rollbackToSealed()
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Delete old segment files
	for _, seg := range segments {
		_ = os.Remove(seg.Filename)
	}

	return nil
}

// mergeRecords reads all records from segments, returning:
// - records: latest INSERT/UPDATE for each live document (not deleted)
// - tombstones: latest DELETE record for each deleted document
// Both must be preserved in compacted output to prevent deleted docs from reappearing.
func (c *Compactor) mergeRecords(segments []SegmentInfo) (map[string]*Record, map[string]*Record, error) {
	records := make(map[string]*Record)    // DocID -> latest INSERT/UPDATE record
	tombstones := make(map[string]*Record) // DocID -> latest DELETE record
	recordLSN := make(map[string]uint64)   // DocID -> LSN of latest record

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
				// Make a copy of the record
				recCopy := *rec
				recCopy.Payload = make([]byte, len(rec.Payload))
				copy(recCopy.Payload, rec.Payload)

				if rec.Type == RecordTypeDelete {
					// Latest operation is DELETE - track as tombstone
					tombstones[docID] = &recCopy
					delete(records, docID)
				} else {
					// Latest operation is INSERT/UPDATE - track as live record
					records[docID] = &recCopy
					delete(tombstones, docID)
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

// ForceCompact compacts all sealed WAL segments regardless of minimum count
func (c *Compactor) ForceCompact(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	segments, err := c.manifest.GetSealedWALSegments(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sealed WAL segments: %w", err)
	}

	if len(segments) < 2 {
		return nil // Need at least 2 WAL segments
	}

	return c.compactSegments(ctx, segments)
}
