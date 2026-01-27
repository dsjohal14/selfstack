package wal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
)

func TestCompactorMergesSegments(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manifest := NewInMemoryManifest()

	// Create segment 1 with some records
	seg1Path := filepath.Join(dir, SegmentFilename(1))
	writer1, err := NewSegmentWriter(seg1Path)
	if err != nil {
		t.Fatalf("failed to create segment writer: %v", err)
	}

	// Write doc-1 and doc-2 to segment 1
	rec1, _ := NewRecord(RecordTypeInsert, 1, mustEncodeDocPayload(t, "doc-1", DocMetadata{Metadata: map[string]string{"v": "1"}}, relay.Embedding{}))
	rec2, _ := NewRecord(RecordTypeInsert, 2, mustEncodeDocPayload(t, "doc-2", DocMetadata{Metadata: map[string]string{"v": "1"}}, relay.Embedding{}))
	_ = writer1.Write(rec1)
	_ = writer1.Write(rec2)
	checksum1, _ := writer1.Finalize()
	_ = writer1.Close()

	// Register segment 1 in manifest
	_ = manifest.CreateSegment(ctx, 1, seg1Path)
	_ = manifest.UpdateSegmentStats(ctx, 1, writer1.Offset(), 2, 1, 2)
	_ = manifest.SealSegment(ctx, 1, checksum1)

	// Create segment 2 with update to doc-1 and delete of doc-2
	seg2Path := filepath.Join(dir, SegmentFilename(2))
	writer2, err := NewSegmentWriter(seg2Path)
	if err != nil {
		t.Fatalf("failed to create segment writer: %v", err)
	}

	// Update doc-1 and delete doc-2
	rec3, _ := NewRecord(RecordTypeUpdate, 3, mustEncodeDocPayload(t, "doc-1", DocMetadata{Metadata: map[string]string{"v": "2"}}, relay.Embedding{}))
	rec4, _ := NewRecord(RecordTypeDelete, 4, mustEncodeDeletePayload(t, "doc-2"))
	_ = writer2.Write(rec3)
	_ = writer2.Write(rec4)
	checksum2, _ := writer2.Finalize()
	_ = writer2.Close()

	// Register segment 2 in manifest
	_ = manifest.CreateSegment(ctx, 2, seg2Path)
	_ = manifest.UpdateSegmentStats(ctx, 2, writer2.Offset(), 2, 3, 4)
	_ = manifest.SealSegment(ctx, 2, checksum2)

	// Verify we have 2 sealed segments
	sealed, _ := manifest.GetSealedWALSegments(ctx)
	if len(sealed) != 2 {
		t.Fatalf("expected 2 sealed segments, got %d", len(sealed))
	}

	// Run compaction (without DB, using manifest-only path)
	compactor := NewCompactor(manifest, nil, dir, CompactorConfig{
		MinSegmentsToCompact:     2,
		MaxSegmentsPerCompaction: 10,
		TmpDir:                   filepath.Join(dir, ".tmp"),
	})

	// Create tmp directory
	_ = os.MkdirAll(filepath.Join(dir, ".tmp"), 0755)

	// Merge records manually (simulating compaction without DB)
	records, tombstones, err := compactor.mergeRecords(sealed)
	if err != nil {
		t.Fatalf("failed to merge records: %v", err)
	}

	// Verify merge results:
	// - doc-1 should have updated metadata (v=2)
	// - doc-2 should be tombstoned
	if len(records) != 1 {
		t.Errorf("expected 1 record after merge, got %d", len(records))
	}

	if !tombstones["doc-2"] {
		t.Error("doc-2 should be tombstoned")
	}

	doc1Rec, ok := records["doc-1"]
	if !ok {
		t.Fatal("doc-1 should exist in merged records")
	}

	// Verify doc-1 has the updated version (LSN 3)
	if doc1Rec.LSN != 3 {
		t.Errorf("expected doc-1 to have LSN 3, got %d", doc1Rec.LSN)
	}

	// Decode and verify metadata
	_, metadata, _, err := DecodeDocPayload(doc1Rec.Payload)
	if err != nil {
		t.Fatalf("failed to decode doc-1 payload: %v", err)
	}
	if metadata.Metadata["v"] != "2" {
		t.Errorf("expected doc-1 metadata v=2, got v=%s", metadata.Metadata["v"])
	}
}

func TestCompactorRecoveryAfterRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manifest := NewInMemoryManifest()
	memIndex := newTestMemIndex()

	// Phase 1: Create initial data
	seg1Path := filepath.Join(dir, SegmentFilename(1))
	writer1, err := NewSegmentWriter(seg1Path)
	if err != nil {
		t.Fatalf("failed to create segment writer: %v", err)
	}

	// Write 5 documents
	for i := 1; i <= 5; i++ {
		rec, _ := NewRecord(RecordTypeInsert, uint64(i), mustEncodeDocPayload(
			t,
			docID(i),
			DocMetadata{Metadata: map[string]string{"index": string(rune('0' + i))}},
			relay.Embedding{},
		))
		_ = writer1.Write(rec)
	}
	checksum1, _ := writer1.Finalize()
	_ = writer1.Close()

	_ = manifest.CreateSegment(ctx, 1, seg1Path)
	_ = manifest.UpdateSegmentStats(ctx, 1, writer1.Offset(), 5, 1, 5)
	_ = manifest.SealSegment(ctx, 1, checksum1)

	// Phase 2: Create second segment with updates/deletes
	seg2Path := filepath.Join(dir, SegmentFilename(2))
	writer2, err := NewSegmentWriter(seg2Path)
	if err != nil {
		t.Fatalf("failed to create segment writer: %v", err)
	}

	// Delete doc-2, update doc-3
	rec6, _ := NewRecord(RecordTypeDelete, 6, mustEncodeDeletePayload(t, "doc-2"))
	rec7, _ := NewRecord(RecordTypeUpdate, 7, mustEncodeDocPayload(t, "doc-3", DocMetadata{Metadata: map[string]string{"updated": "true"}}, relay.Embedding{}))
	_ = writer2.Write(rec6)
	_ = writer2.Write(rec7)
	checksum2, _ := writer2.Finalize()
	_ = writer2.Close()

	_ = manifest.CreateSegment(ctx, 2, seg2Path)
	_ = manifest.UpdateSegmentStats(ctx, 2, writer2.Offset(), 2, 6, 7)
	_ = manifest.SealSegment(ctx, 2, checksum2)

	// Phase 3: Simulate recovery by reading all segments
	recovery := NewRecoveryManager(manifest, dir, memIndex)

	stats, err := recovery.Recover(ctx)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	// Verify recovery stats
	if stats.SegmentsLoaded != 2 {
		t.Errorf("expected 2 segments loaded, got %d", stats.SegmentsLoaded)
	}

	// Verify recovered state:
	// - doc-1: exists (insert)
	// - doc-2: deleted
	// - doc-3: exists with updated metadata
	// - doc-4: exists (insert)
	// - doc-5: exists (insert)

	if memIndex.count != 4 {
		t.Errorf("expected 4 documents after recovery, got %d", memIndex.count)
	}

	// doc-2 should not exist
	if _, exists := memIndex.docs["doc-2"]; exists {
		t.Error("doc-2 should be deleted")
	}

	// doc-3 should have updated metadata
	if !memIndex.Has("doc-3") {
		t.Fatal("doc-3 should exist")
	}
	doc3 := memIndex.docs["doc-3"]
	if doc3.Metadata["updated"] != "true" {
		t.Errorf("doc-3 should have updated=true, got %v", doc3.Metadata)
	}
}

func TestCompactorWithCompactedSegments(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	manifest := NewInMemoryManifest()
	memIndex := newTestMemIndex()

	// Create a WAL segment
	walPath := filepath.Join(dir, SegmentFilename(1))
	writer1, err := NewSegmentWriter(walPath)
	if err != nil {
		t.Fatalf("failed to create segment writer: %v", err)
	}

	rec1, _ := NewRecord(RecordTypeInsert, 1, mustEncodeDocPayload(t, "doc-1", DocMetadata{}, relay.Embedding{}))
	_ = writer1.Write(rec1)
	checksum1, _ := writer1.Finalize()
	_ = writer1.Close()

	_ = manifest.CreateSegment(ctx, 1, walPath)
	_ = manifest.UpdateSegmentStats(ctx, 1, writer1.Offset(), 1, 1, 1)
	_ = manifest.SealSegment(ctx, 1, checksum1)

	// Create a compacted segment with the same ID (should be in separate namespace)
	cmpPath := filepath.Join(dir, CompactedSegmentFilename(1))
	writer2, err := NewSegmentWriter(cmpPath)
	if err != nil {
		t.Fatalf("failed to create compacted segment writer: %v", err)
	}

	rec2, _ := NewRecord(RecordTypeInsert, 2, mustEncodeDocPayload(t, "doc-2", DocMetadata{}, relay.Embedding{}))
	_ = writer2.Write(rec2)
	checksum2, _ := writer2.Finalize()
	_ = writer2.Close()

	_ = manifest.CreateCompactedSegment(ctx, 1, cmpPath, writer2.Offset(), 1, 2, 2, checksum2)

	// GetSealedWALSegments should only return the WAL segment
	walSegs, _ := manifest.GetSealedWALSegments(ctx)
	if len(walSegs) != 1 {
		t.Errorf("expected 1 WAL segment, got %d", len(walSegs))
	}
	if walSegs[0].SegmentType != SegmentTypeWAL {
		t.Errorf("expected WAL segment type, got %s", walSegs[0].SegmentType)
	}

	// GetSealedSegments should return both
	allSegs, _ := manifest.GetSealedSegments(ctx)
	if len(allSegs) != 2 {
		t.Errorf("expected 2 total segments, got %d", len(allSegs))
	}

	// Recovery should read both segments
	recovery := NewRecoveryManager(manifest, dir, memIndex)

	stats, err := recovery.Recover(ctx)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	if stats.RecordsLoaded != 2 {
		t.Errorf("expected 2 records loaded, got %d", stats.RecordsLoaded)
	}

	if memIndex.count != 2 {
		t.Errorf("expected 2 documents, got %d", memIndex.count)
	}
}

// Test helper types and functions

// testMemIndex implements DocumentIndex for testing
type testMemIndex struct {
	docs  map[string]*RecoveredDoc
	count int
}

func newTestMemIndex() *testMemIndex {
	return &testMemIndex{
		docs: make(map[string]*RecoveredDoc),
	}
}

func (m *testMemIndex) SetRecovered(doc RecoveredDoc) {
	if _, exists := m.docs[doc.DocID]; !exists {
		m.count++
	}
	m.docs[doc.DocID] = &doc
}

func (m *testMemIndex) Delete(id string) {
	if _, exists := m.docs[id]; exists {
		delete(m.docs, id)
		m.count--
	}
}

func (m *testMemIndex) Has(id string) bool {
	_, exists := m.docs[id]
	return exists
}

func (m *testMemIndex) Count() int {
	return m.count
}

func docID(i int) string {
	return "doc-" + string(rune('0'+i))
}

func mustEncodeDocPayload(t *testing.T, docID string, meta DocMetadata, embedding relay.Embedding) []byte {
	t.Helper()
	meta.CreatedAt = time.Now()
	payload, err := EncodeDocPayload(docID, meta, embedding)
	if err != nil {
		t.Fatalf("failed to encode doc payload: %v", err)
	}
	return payload
}

func mustEncodeDeletePayload(t *testing.T, docID string) []byte {
	t.Helper()
	payload, err := EncodeDeletePayload(docID)
	if err != nil {
		t.Fatalf("failed to encode delete payload: %v", err)
	}
	return payload
}
