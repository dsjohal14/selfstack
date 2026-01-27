package wal

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryManifest(t *testing.T) {
	ctx := context.Background()
	manifest := NewInMemoryManifest()

	// Create segment
	err := manifest.CreateSegment(ctx, 1, "/path/to/segment1.seg")
	if err != nil {
		t.Fatalf("failed to create segment: %v", err)
	}

	// Get active segment
	seg, err := manifest.GetActiveSegment(ctx)
	if err != nil {
		t.Fatalf("failed to get active segment: %v", err)
	}
	if seg == nil {
		t.Fatal("expected active segment, got nil")
	}
	if seg.SegmentID != 1 {
		t.Errorf("expected segment ID 1, got %d", seg.SegmentID)
	}
	if seg.Status != SegmentStatusActive {
		t.Errorf("expected status active, got %s", seg.Status)
	}

	// Seal segment
	err = manifest.SealSegment(ctx, 1, "checksum123")
	if err != nil {
		t.Fatalf("failed to seal segment: %v", err)
	}

	// Get sealed segments
	sealed, err := manifest.GetSealedSegments(ctx)
	if err != nil {
		t.Fatalf("failed to get sealed segments: %v", err)
	}
	if len(sealed) != 1 {
		t.Errorf("expected 1 sealed segment, got %d", len(sealed))
	}
	if *sealed[0].Checksum != "checksum123" {
		t.Errorf("expected checksum checksum123, got %s", *sealed[0].Checksum)
	}
}

func TestInMemoryManifestWALState(t *testing.T) {
	ctx := context.Background()
	manifest := NewInMemoryManifest()

	// Get initial state
	state, err := manifest.GetWALState(ctx)
	if err != nil {
		t.Fatalf("failed to get WAL state: %v", err)
	}
	if state.CurrentSegmentID != 1 {
		t.Errorf("expected current segment ID 1, got %d", state.CurrentSegmentID)
	}
	if state.NextLSN != 1 {
		t.Errorf("expected next LSN 1, got %d", state.NextLSN)
	}

	// Update state
	err = manifest.UpdateWALState(ctx, 5, 100)
	if err != nil {
		t.Fatalf("failed to update WAL state: %v", err)
	}

	// Verify update
	state, err = manifest.GetWALState(ctx)
	if err != nil {
		t.Fatalf("failed to get WAL state: %v", err)
	}
	if state.CurrentSegmentID != 5 {
		t.Errorf("expected current segment ID 5, got %d", state.CurrentSegmentID)
	}
	if state.NextLSN != 100 {
		t.Errorf("expected next LSN 100, got %d", state.NextLSN)
	}

	// Update checkpoint
	err = manifest.UpdateCheckpointLSN(ctx, 50)
	if err != nil {
		t.Fatalf("failed to update checkpoint LSN: %v", err)
	}

	state, err = manifest.GetWALState(ctx)
	if err != nil {
		t.Fatalf("failed to get WAL state: %v", err)
	}
	if state.CheckpointLSN != 50 {
		t.Errorf("expected checkpoint LSN 50, got %d", state.CheckpointLSN)
	}
}

func TestInMemoryManifestRecoveryInfo(t *testing.T) {
	ctx := context.Background()
	manifest := NewInMemoryManifest()

	// Create and seal some segments
	_ = manifest.CreateSegment(ctx, 1, "/path/seg1.seg")
	_ = manifest.SealSegment(ctx, 1, "cs1")

	_ = manifest.CreateSegment(ctx, 2, "/path/seg2.seg")
	_ = manifest.SealSegment(ctx, 2, "cs2")

	_ = manifest.CreateSegment(ctx, 3, "/path/seg3.seg") // Active

	// Get recovery info
	info, err := manifest.GetRecoveryInfo(ctx)
	if err != nil {
		t.Fatalf("failed to get recovery info: %v", err)
	}

	if len(info.Segments) != 3 {
		t.Errorf("expected 3 segments, got %d", len(info.Segments))
	}

	// Archive one segment
	_ = manifest.ArchiveSegments(ctx, []uint64{1})

	// Get recovery info again (archived segments excluded)
	info, err = manifest.GetRecoveryInfo(ctx)
	if err != nil {
		t.Fatalf("failed to get recovery info: %v", err)
	}

	if len(info.Segments) != 2 {
		t.Errorf("expected 2 non-archived segments, got %d", len(info.Segments))
	}
}

func TestInMemoryManifestSegmentStats(t *testing.T) {
	ctx := context.Background()
	manifest := NewInMemoryManifest()

	// Create segment
	_ = manifest.CreateSegment(ctx, 1, "/path/seg1.seg")

	// Update stats
	err := manifest.UpdateSegmentStats(ctx, 1, 1024, 100, 1, 100)
	if err != nil {
		t.Fatalf("failed to update segment stats: %v", err)
	}

	// Verify
	key := segmentKey{Type: SegmentTypeWAL, ID: 1}
	seg := manifest.segments[key]
	if seg.SizeBytes != 1024 {
		t.Errorf("expected size 1024, got %d", seg.SizeBytes)
	}
	if seg.RecordCount != 100 {
		t.Errorf("expected record count 100, got %d", seg.RecordCount)
	}
	if *seg.MinLSN != 1 {
		t.Errorf("expected min LSN 1, got %d", *seg.MinLSN)
	}
	if *seg.MaxLSN != 100 {
		t.Errorf("expected max LSN 100, got %d", *seg.MaxLSN)
	}
}

func TestSegmentStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	manifest := NewInMemoryManifest()

	// Create -> Active
	_ = manifest.CreateSegment(ctx, 1, "/path/seg1.seg")
	segs, _ := manifest.GetSegmentsByStatus(ctx, SegmentStatusActive)
	if len(segs) != 1 {
		t.Errorf("expected 1 active segment")
	}

	// Seal -> Sealed
	_ = manifest.SealSegment(ctx, 1, "checksum")
	segs, _ = manifest.GetSegmentsByStatus(ctx, SegmentStatusSealed)
	if len(segs) != 1 {
		t.Errorf("expected 1 sealed segment")
	}

	// Update status -> Compacting
	_ = manifest.UpdateSegmentStatus(ctx, 1, SegmentStatusCompacting)
	segs, _ = manifest.GetSegmentsByStatus(ctx, SegmentStatusCompacting)
	if len(segs) != 1 {
		t.Errorf("expected 1 compacting segment")
	}

	// Archive
	_ = manifest.ArchiveSegments(ctx, []uint64{1})
	segs, _ = manifest.GetSegmentsByStatus(ctx, SegmentStatusArchived)
	if len(segs) != 1 {
		t.Errorf("expected 1 archived segment")
	}
}

func TestSegmentInfoFields(t *testing.T) {
	seg := SegmentInfo{
		ID:          1,
		SegmentID:   100,
		Filename:    "/path/to/segment.seg",
		SizeBytes:   1024,
		RecordCount: 50,
		Status:      SegmentStatusSealed,
		CreatedAt:   time.Now(),
	}

	if seg.SegmentID != 100 {
		t.Errorf("segment ID mismatch")
	}
	if seg.Status != SegmentStatusSealed {
		t.Errorf("status mismatch")
	}
}
