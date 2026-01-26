package wal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SegmentRoller handles segment lifecycle operations
type SegmentRoller struct {
	dir         string
	manifest    ManifestStore
	maxSize     int64
	maxAge      time.Duration // Max age before forcing rotation (0 = disabled)
	maxSegments int           // Max number of sealed segments before cleanup (0 = disabled)
}

// SegmentRollerOption configures a SegmentRoller
type SegmentRollerOption func(*SegmentRoller)

// WithMaxAge sets the maximum segment age before forced rotation
func WithMaxAge(d time.Duration) SegmentRollerOption {
	return func(r *SegmentRoller) {
		r.maxAge = d
	}
}

// WithMaxSegments sets the max number of sealed segments to keep
func WithMaxSegments(n int) SegmentRollerOption {
	return func(r *SegmentRoller) {
		r.maxSegments = n
	}
}

// NewSegmentRoller creates a new segment roller
func NewSegmentRoller(dir string, manifest ManifestStore, opts ...SegmentRollerOption) *SegmentRoller {
	r := &SegmentRoller{
		dir:      dir,
		manifest: manifest,
		maxSize:  DefaultMaxSegmentSize,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// ShouldRotate checks if a segment should be rotated based on size and age
func (r *SegmentRoller) ShouldRotate(segmentPath string, createdAt time.Time) (bool, string, error) {
	// Check size
	stat, err := os.Stat(segmentPath)
	if err != nil {
		return false, "", fmt.Errorf("failed to stat segment: %w", err)
	}

	if stat.Size() >= r.maxSize {
		return true, "size limit exceeded", nil
	}

	// Check age
	if r.maxAge > 0 && time.Since(createdAt) >= r.maxAge {
		return true, "age limit exceeded", nil
	}

	return false, "", nil
}

// ListSegmentFiles returns all segment files in the WAL directory
func (r *SegmentRoller) ListSegmentFiles() ([]string, error) {
	return ListSegmentFiles(r.dir)
}

// ListSegmentFiles returns all segment files in a directory, sorted by segment ID
// Includes both WAL segments (wal_) and compacted segments (cmp_)
func ListSegmentFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var segments []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Include both WAL segments and compacted segments
		isWAL := strings.HasPrefix(name, "wal_") && strings.HasSuffix(name, ".seg")
		isCompacted := strings.HasPrefix(name, "cmp_") && strings.HasSuffix(name, ".seg")
		if isWAL || isCompacted {
			segments = append(segments, filepath.Join(dir, name))
		}
	}

	// Sort by extracted segment ID to ensure proper ordering
	sort.Slice(segments, func(i, j int) bool {
		idI, _ := GetSegmentID(segments[i])
		idJ, _ := GetSegmentID(segments[j])
		return idI < idJ
	})
	return segments, nil
}

// CleanupOldSegments removes archived segments that exceed the retention limit
func (r *SegmentRoller) CleanupOldSegments(ctx context.Context) (int, error) {
	if r.maxSegments <= 0 || r.manifest == nil {
		return 0, nil
	}

	segments, err := r.manifest.GetSegmentsByStatus(ctx, SegmentStatusArchived)
	if err != nil {
		return 0, fmt.Errorf("failed to get archived segments: %w", err)
	}

	if len(segments) <= r.maxSegments {
		return 0, nil
	}

	// Sort by segment ID (oldest first)
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].SegmentID < segments[j].SegmentID
	})

	// Delete oldest segments
	toDelete := segments[:len(segments)-r.maxSegments]
	deleted := 0

	for _, seg := range toDelete {
		// Delete file
		if err := os.Remove(seg.Filename); err != nil && !os.IsNotExist(err) {
			return deleted, fmt.Errorf("failed to delete segment file %s: %w", seg.Filename, err)
		}
		deleted++
	}

	return deleted, nil
}

// GetSegmentID extracts the segment ID from a segment filename
// Supports both WAL segments (wal_) and compacted segments (cmp_)
func GetSegmentID(filename string) (uint64, error) {
	base := filepath.Base(filename)
	var id uint64

	// Try WAL segment format
	n, err := fmt.Sscanf(base, "wal_%d.seg", &id)
	if err == nil && n == 1 {
		return id, nil
	}

	// Try compacted segment format
	n, err = fmt.Sscanf(base, "cmp_%d.seg", &id)
	if err == nil && n == 1 {
		return id, nil
	}

	return 0, fmt.Errorf("invalid segment filename: %s", filename)
}

// SegmentFilename generates a WAL segment filename for a given ID
func SegmentFilename(segmentID uint64) string {
	return fmt.Sprintf("wal_%012d.seg", segmentID)
}

// CompactedSegmentFilename generates a compacted segment filename for a given ID
// Compacted segments use a separate namespace (cmp_) to avoid ID collisions with
// the live WAL writer during rotation
func CompactedSegmentFilename(segmentID uint64) string {
	return fmt.Sprintf("cmp_%012d.seg", segmentID)
}

// FindLatestSegment finds the segment with the highest ID in a directory
func FindLatestSegment(dir string) (string, uint64, error) {
	segments, err := ListSegmentFiles(dir)
	if err != nil {
		return "", 0, err
	}

	if len(segments) == 0 {
		return "", 0, nil
	}

	latest := segments[len(segments)-1]
	id, err := GetSegmentID(latest)
	if err != nil {
		return "", 0, err
	}

	return latest, id, nil
}

// FindSegmentsInRange finds all segments that may contain records in the LSN range
func FindSegmentsInRange(ctx context.Context, manifest ManifestStore, minLSN, maxLSN uint64) ([]SegmentInfo, error) {
	info, err := manifest.GetRecoveryInfo(ctx)
	if err != nil {
		return nil, err
	}

	var result []SegmentInfo
	for _, seg := range info.Segments {
		// Include segment if LSN ranges overlap
		segMin := uint64(0)
		segMax := uint64(^uint64(0))
		if seg.MinLSN != nil {
			segMin = *seg.MinLSN
		}
		if seg.MaxLSN != nil {
			segMax = *seg.MaxLSN
		}

		// Check for overlap
		if segMax >= minLSN && segMin <= maxLSN {
			result = append(result, seg)
		}
	}

	return result, nil
}
