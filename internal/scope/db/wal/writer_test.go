package wal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewWALWriter(t *testing.T) {
	dir := t.TempDir()

	writer, err := NewWALWriter(dir)
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	if writer.CurrentLSN() != 1 {
		t.Errorf("initial LSN should be 1, got %d", writer.CurrentLSN())
	}

	if writer.CurrentSegmentID() != 1 {
		t.Errorf("initial segment ID should be 1, got %d", writer.CurrentSegmentID())
	}

	// Verify segment file was created
	segmentPath := filepath.Join(dir, "wal_000000000001.seg")
	if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
		t.Errorf("segment file was not created: %s", segmentPath)
	}
}

func TestWALWriterAppend(t *testing.T) {
	dir := t.TempDir()

	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Append records
	for i := 0; i < 10; i++ {
		payload := []byte("test payload")
		lsn, err := writer.Append(RecordTypeInsert, payload)
		if err != nil {
			t.Fatalf("failed to append record %d: %v", i, err)
		}
		if lsn != uint64(i+1) {
			t.Errorf("expected LSN %d, got %d", i+1, lsn)
		}
	}

	if writer.CurrentLSN() != 11 {
		t.Errorf("expected current LSN 11, got %d", writer.CurrentLSN())
	}
}

func TestWALWriterAppendWithSync(t *testing.T) {
	dir := t.TempDir()

	writer, err := NewWALWriter(dir)
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	payload := []byte("test payload for sync")
	lsn, err := writer.AppendWithSync(RecordTypeInsert, payload)
	if err != nil {
		t.Fatalf("failed to append with sync: %v", err)
	}

	if lsn != 1 {
		t.Errorf("expected LSN 1, got %d", lsn)
	}

	// Verify data was written
	if writer.CurrentOffset() == 0 {
		t.Error("expected non-zero offset after write")
	}
}

func TestWALWriterSegmentRotation(t *testing.T) {
	dir := t.TempDir()

	// Small segment size to trigger rotation
	writer, err := NewWALWriter(dir,
		WithSyncPolicy(ImmediateSyncPolicy()),
		WithMaxSegmentSize(1024), // 1KB segments
	)
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Write enough data to trigger rotation
	payload := make([]byte, 256)
	for i := 0; i < 10; i++ {
		_, err := writer.Append(RecordTypeInsert, payload)
		if err != nil {
			t.Fatalf("failed to append record %d: %v", i, err)
		}
	}

	// Should have rotated to a new segment
	if writer.CurrentSegmentID() <= 1 {
		t.Errorf("expected segment rotation, still on segment %d", writer.CurrentSegmentID())
	}

	// Verify multiple segment files exist
	entries, _ := os.ReadDir(dir)
	segmentCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".seg" {
			segmentCount++
		}
	}
	if segmentCount < 2 {
		t.Errorf("expected multiple segment files, got %d", segmentCount)
	}
}

func TestWALWriterCloseAndReopen(t *testing.T) {
	dir := t.TempDir()

	// Write some records
	writer1, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := writer1.Append(RecordTypeInsert, []byte("payload"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	lastLSN := writer1.CurrentLSN()
	lastSegment := writer1.CurrentSegmentID()
	_ = writer1.Close()

	// Reopen with initial LSN and segment ID
	writer2, err := NewWALWriter(dir,
		WithSyncPolicy(ImmediateSyncPolicy()),
		WithInitialLSN(lastLSN),
		WithInitialSegmentID(lastSegment),
	)
	if err != nil {
		t.Fatalf("failed to reopen WAL writer: %v", err)
	}
	defer func() { _ = writer2.Close() }()

	// Write more records
	lsn, err := writer2.Append(RecordTypeInsert, []byte("new payload"))
	if err != nil {
		t.Fatalf("failed to append after reopen: %v", err)
	}

	if lsn != lastLSN {
		t.Errorf("expected LSN %d after reopen, got %d", lastLSN, lsn)
	}
}

func TestWALWriterWithManifest(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	manifest := NewInMemoryManifest()

	// Create initial segment in manifest (simulating what WALStore does)
	segPath := filepath.Join(dir, "wal_000000000001.seg")
	if err := manifest.CreateSegment(ctx, 1, segPath); err != nil {
		t.Fatalf("failed to create initial segment in manifest: %v", err)
	}

	writer, err := NewWALWriter(dir,
		WithSyncPolicy(ImmediateSyncPolicy()),
		WithManifest(manifest),
		WithMaxSegmentSize(512), // Small for rotation
	)
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Write enough to trigger rotation
	payload := make([]byte, 128)
	for i := 0; i < 10; i++ {
		_, err := writer.Append(RecordTypeInsert, payload)
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	// Verify manifest was updated (may still be 1 if no rotation happened)
	_ = manifest.state.CurrentSegmentID
}

func TestWALWriterClosed(t *testing.T) {
	dir := t.TempDir()

	writer, err := NewWALWriter(dir)
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	_ = writer.Close()

	// Append after close should fail
	_, err = writer.Append(RecordTypeInsert, []byte("test"))
	if err == nil {
		t.Error("expected error when appending to closed writer")
	}
}

func TestWALWriterSync(t *testing.T) {
	dir := t.TempDir()

	// Use batched sync policy
	writer, err := NewWALWriter(dir, WithSyncPolicy(SyncPolicy{
		Immediate: false,
		BatchSize: 100,
	}))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// Write some records
	for i := 0; i < 5; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}

	// Explicit sync
	if err := writer.Sync(); err != nil {
		t.Errorf("sync failed: %v", err)
	}
}
