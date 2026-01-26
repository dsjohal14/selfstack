package wal

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
)

// RecoveryStats contains statistics from the recovery process
type RecoveryStats struct {
	SegmentsLoaded     int
	RecordsLoaded      int
	WALRecordsReplayed int
	TombstonesApplied  int
	CorruptRecords     int
	RecoveryTime       time.Duration
	MaxLSN             uint64
}

// RecoveryManager handles WAL recovery on cold start
type RecoveryManager struct {
	manifest ManifestStore
	walDir   string
	index    DocumentIndex
}

// RecoveredDoc represents a document recovered from the WAL
type RecoveredDoc struct {
	DocID     string
	Source    string
	Title     string
	Text      string
	Metadata  map[string]string
	CreatedAt time.Time
	Embedding relay.Embedding
}

// DocumentIndex is the interface for the in-memory document index
type DocumentIndex interface {
	SetRecovered(doc RecoveredDoc)
	Delete(docID string)
	Has(docID string) bool
	Count() int
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(manifest ManifestStore, walDir string, index DocumentIndex) *RecoveryManager {
	return &RecoveryManager{
		manifest: manifest,
		walDir:   walDir,
		index:    index,
	}
}

// Recover rebuilds the in-memory index from WAL segments
func (r *RecoveryManager) Recover(ctx context.Context) (*RecoveryStats, error) {
	startTime := time.Now()
	stats := &RecoveryStats{}

	// Get recovery info from manifest
	info, err := r.manifest.GetRecoveryInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get recovery info: %w", err)
	}

	// Sort segments by segment ID
	sort.Slice(info.Segments, func(i, j int) bool {
		return info.Segments[i].SegmentID < info.Segments[j].SegmentID
	})

	// Track documents and tombstones
	docLSN := make(map[string]uint64) // DocID -> highest LSN seen

	// Load records from sealed segments
	for _, seg := range info.Segments {
		if seg.Status == SegmentStatusArchived {
			continue
		}

		// Verify checksum for sealed segments
		if seg.Status == SegmentStatusSealed && seg.Checksum != nil {
			valid, err := r.verifySegment(seg)
			if err != nil {
				return nil, fmt.Errorf("failed to verify segment %s: %w", seg.Filename, err)
			}
			if !valid {
				return nil, fmt.Errorf("corrupt segment detected: %s", seg.Filename)
			}
		}

		// Check if file exists
		if _, err := os.Stat(seg.Filename); os.IsNotExist(err) {
			// Segment file missing - this is a problem for sealed segments
			if seg.Status == SegmentStatusSealed {
				return nil, fmt.Errorf("missing sealed segment file: %s", seg.Filename)
			}
			continue
		}

		// Read records from segment
		iter, err := NewSegmentIteratorFromLSN(seg.Filename, info.State.CheckpointLSN+1)
		if err != nil {
			return nil, fmt.Errorf("failed to open segment %s: %w", seg.Filename, err)
		}

		for iter.Next() {
			rec := iter.Record()
			stats.RecordsLoaded++

			if rec.LSN > stats.MaxLSN {
				stats.MaxLSN = rec.LSN
			}

			if err := r.applyRecord(rec, docLSN); err != nil {
				stats.CorruptRecords++
				// Log but continue - partial recovery is better than none
				fmt.Printf("warning: failed to apply record at LSN %d: %v\n", rec.LSN, err)
				continue
			}

			if rec.Type == RecordTypeDelete {
				stats.TombstonesApplied++
			}
		}

		if err := iter.Err(); err != nil {
			_ = iter.Close()
			return nil, fmt.Errorf("error reading segment %s: %w", seg.Filename, err)
		}
		_ = iter.Close()
		stats.SegmentsLoaded++
	}

	// Replay active WAL if present
	activeSegment, err := r.findActiveSegment(info)
	if err != nil {
		return nil, fmt.Errorf("failed to find active segment: %w", err)
	}

	if activeSegment != "" {
		replayedRecords, err := r.replayActiveWAL(activeSegment, info.State.CheckpointLSN, docLSN, stats)
		if err != nil {
			return nil, fmt.Errorf("failed to replay active WAL: %w", err)
		}
		stats.WALRecordsReplayed = replayedRecords
	}

	stats.RecoveryTime = time.Since(startTime)
	return stats, nil
}

// verifySegment verifies the checksum of a sealed segment
func (r *RecoveryManager) verifySegment(seg SegmentInfo) (bool, error) {
	if seg.Checksum == nil {
		return true, nil // No checksum to verify
	}
	return VerifySegmentChecksum(seg.Filename, *seg.Checksum)
}

// findActiveSegment finds the current active WAL segment file
func (r *RecoveryManager) findActiveSegment(info *RecoveryInfo) (string, error) {
	// Look for active segment in manifest
	for _, seg := range info.Segments {
		if seg.Status == SegmentStatusActive {
			if _, err := os.Stat(seg.Filename); err == nil {
				return seg.Filename, nil
			}
		}
	}

	// Fall back to looking for the file based on current segment ID
	segmentPath := fmt.Sprintf("%s/wal_%012d.seg", r.walDir, info.State.CurrentSegmentID)
	if _, err := os.Stat(segmentPath); err == nil {
		return segmentPath, nil
	}

	return "", nil
}

// replayActiveWAL replays records from the active WAL segment
func (r *RecoveryManager) replayActiveWAL(walPath string, checkpointLSN uint64, docLSN map[string]uint64, stats *RecoveryStats) (int, error) {
	iter, err := NewSegmentIteratorFromLSN(walPath, checkpointLSN+1)
	if err != nil {
		return 0, fmt.Errorf("failed to open active WAL: %w", err)
	}
	defer func() { _ = iter.Close() }()

	replayed := 0
	for iter.Next() {
		rec := iter.Record()

		if err := r.applyRecord(rec, docLSN); err != nil {
			// On corruption in active WAL, truncate here
			// This record and all following are lost
			fmt.Printf("warning: corruption detected at LSN %d, truncating WAL\n", rec.LSN)
			break
		}

		if rec.LSN > stats.MaxLSN {
			stats.MaxLSN = rec.LSN
		}
		replayed++
	}

	// Don't fail on error in active WAL - just stop at corruption point
	if err := iter.Err(); err != nil {
		fmt.Printf("warning: error reading active WAL (truncated at corruption): %v\n", err)
	}

	return replayed, nil
}

// applyRecord applies a record to the in-memory index
func (r *RecoveryManager) applyRecord(rec *Record, docLSN map[string]uint64) error {
	switch rec.Type {
	case RecordTypeInsert, RecordTypeUpdate:
		docID, meta, embedding, err := DecodeDocPayload(rec.Payload)
		if err != nil {
			return fmt.Errorf("failed to decode payload: %w", err)
		}

		// Only apply if this is the latest record for this document
		if existingLSN, exists := docLSN[docID]; exists && existingLSN >= rec.LSN {
			return nil // Stale record
		}
		docLSN[docID] = rec.LSN

		doc := RecoveredDoc{
			DocID:     docID,
			Source:    meta.Source,
			Title:     meta.Title,
			Text:      meta.Text,
			Metadata:  meta.Metadata,
			CreatedAt: meta.CreatedAt,
			Embedding: embedding,
		}
		r.index.SetRecovered(doc)

	case RecordTypeDelete:
		docID, err := DecodeDeletePayload(rec.Payload)
		if err != nil {
			return fmt.Errorf("failed to decode delete payload: %w", err)
		}

		// Only apply if this is the latest record for this document
		if existingLSN, exists := docLSN[docID]; exists && existingLSN >= rec.LSN {
			return nil // Stale record
		}
		docLSN[docID] = rec.LSN
		r.index.Delete(docID)

	case RecordTypeCheckpoint:
		// Checkpoint records are informational, no action needed

	default:
		// Unknown record type - skip
	}

	return nil
}

// RecoverWithoutManifest performs recovery when no manifest is available
// Uses file system scan to find segments
func (r *RecoveryManager) RecoverWithoutManifest(_ context.Context) (*RecoveryStats, error) {
	startTime := time.Now()
	stats := &RecoveryStats{}

	// Scan WAL directory for segment files
	segments, err := ListSegmentFiles(r.walDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list segment files: %w", err)
	}

	if len(segments) == 0 {
		stats.RecoveryTime = time.Since(startTime)
		return stats, nil
	}

	docLSN := make(map[string]uint64)

	// Process segments in order
	for _, segPath := range segments {
		iter, err := NewSegmentIterator(segPath)
		if err != nil {
			// Can't open segment - log and continue to next
			fmt.Printf("warning: failed to open segment %s: %v\n", segPath, err)
			continue
		}

		segmentCorrupt := false
		segmentRecords := 0 // Per-segment count for accurate logging
		for iter.Next() {
			rec := iter.Record()
			stats.RecordsLoaded++
			segmentRecords++

			if rec.LSN > stats.MaxLSN {
				stats.MaxLSN = rec.LSN
			}

			if err := r.applyRecord(rec, docLSN); err != nil {
				stats.CorruptRecords++
				// Continue trying to read more records (corruption may be isolated)
				continue
			}

			if rec.Type == RecordTypeDelete {
				stats.TombstonesApplied++
			}
		}

		if err := iter.Err(); err != nil {
			// Iterator error - likely corruption at current position
			// For tail corruption (crash scenario), this is expected
			// For mid-segment CRC corruption, iterator stops here (no magic-byte resync)
			stats.CorruptRecords++
			segmentCorrupt = true
			fmt.Printf("warning: error reading segment %s (recovered %d records from this segment before error): %v\n",
				segPath, segmentRecords, err)
		}
		_ = iter.Close()

		if !segmentCorrupt {
			stats.SegmentsLoaded++
		}
	}

	stats.RecoveryTime = time.Since(startTime)
	return stats, nil
}

// ToRecoveredDoc converts DocMetadata + embedding to RecoveredDoc
func ToRecoveredDoc(docID string, meta DocMetadata, embedding relay.Embedding) RecoveredDoc {
	return RecoveredDoc{
		DocID:     docID,
		Source:    meta.Source,
		Title:     meta.Title,
		Text:      meta.Text,
		Metadata:  meta.Metadata,
		CreatedAt: meta.CreatedAt,
		Embedding: embedding,
	}
}
