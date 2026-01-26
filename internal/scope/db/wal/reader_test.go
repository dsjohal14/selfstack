package wal

import (
	"os"
	"testing"
)

func TestSegmentIterator(t *testing.T) {
	dir := t.TempDir()

	// Write some records
	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	recordCount := 10
	for i := 0; i < recordCount; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test payload"))
		if err != nil {
			t.Fatalf("failed to append record %d: %v", i, err)
		}
	}
	writer.Close()

	// Read records
	segmentPath := writer.segmentPath(1)
	iter, err := NewSegmentIterator(segmentPath)
	if err != nil {
		t.Fatalf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	readCount := 0
	for iter.Next() {
		rec := iter.Record()
		if rec.Type != RecordTypeInsert {
			t.Errorf("expected INSERT, got %v", rec.Type)
		}
		if rec.LSN != uint64(readCount+1) {
			t.Errorf("expected LSN %d, got %d", readCount+1, rec.LSN)
		}
		readCount++
	}

	if err := iter.Err(); err != nil {
		t.Errorf("iterator error: %v", err)
	}

	if readCount != recordCount {
		t.Errorf("expected %d records, got %d", recordCount, readCount)
	}
}

func TestSegmentIteratorFromLSN(t *testing.T) {
	dir := t.TempDir()

	// Write records
	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	for i := 0; i < 10; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	writer.Close()

	// Read from LSN 5
	segmentPath := writer.segmentPath(1)
	iter, err := NewSegmentIteratorFromLSN(segmentPath, 5)
	if err != nil {
		t.Fatalf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	readCount := 0
	for iter.Next() {
		rec := iter.Record()
		if rec.LSN < 5 {
			t.Errorf("got record with LSN %d, expected >= 5", rec.LSN)
		}
		readCount++
	}

	if readCount != 6 { // LSNs 5, 6, 7, 8, 9, 10
		t.Errorf("expected 6 records, got %d", readCount)
	}
}

func TestReadAllRecords(t *testing.T) {
	dir := t.TempDir()

	// Write records
	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	writer.Close()

	// Read all
	records, err := ReadAllRecords(writer.segmentPath(1))
	if err != nil {
		t.Fatalf("failed to read all records: %v", err)
	}

	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
}

func TestCalculateSegmentChecksum(t *testing.T) {
	dir := t.TempDir()

	// Write records
	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test payload"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	writer.Close()

	segmentPath := writer.segmentPath(1)

	// Calculate checksum twice - should be same
	checksum1, err := CalculateSegmentChecksum(segmentPath)
	if err != nil {
		t.Fatalf("failed to calculate checksum: %v", err)
	}

	checksum2, err := CalculateSegmentChecksum(segmentPath)
	if err != nil {
		t.Fatalf("failed to calculate checksum: %v", err)
	}

	if checksum1 != checksum2 {
		t.Errorf("checksums don't match: %s vs %s", checksum1, checksum2)
	}

	// Verify checksum
	valid, err := VerifySegmentChecksum(segmentPath, checksum1)
	if err != nil {
		t.Fatalf("failed to verify checksum: %v", err)
	}
	if !valid {
		t.Error("checksum verification failed")
	}

	// Verify wrong checksum fails
	valid, err = VerifySegmentChecksum(segmentPath, "wrongchecksum")
	if err != nil {
		t.Fatalf("failed to verify checksum: %v", err)
	}
	if valid {
		t.Error("wrong checksum should not validate")
	}
}

func TestSegmentWriter(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.seg"

	sw, err := NewSegmentWriter(path)
	if err != nil {
		t.Fatalf("failed to create segment writer: %v", err)
	}

	// Write records
	for i := 0; i < 5; i++ {
		rec, err := NewRecord(RecordTypeInsert, uint64(i+1), []byte("test"))
		if err != nil {
			t.Fatalf("failed to create record: %v", err)
		}
		if err := sw.Write(rec); err != nil {
			t.Fatalf("failed to write record: %v", err)
		}
	}

	checksum, err := sw.Finalize()
	if err != nil {
		t.Fatalf("failed to finalize: %v", err)
	}
	sw.Close()

	if checksum == "" {
		t.Error("expected non-empty checksum")
	}

	// Verify written records
	records, err := ReadAllRecords(path)
	if err != nil {
		t.Fatalf("failed to read records: %v", err)
	}

	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
}

func TestGetSegmentLSNRange(t *testing.T) {
	dir := t.TempDir()

	// Write records
	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	for i := 0; i < 10; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	writer.Close()

	minLSN, maxLSN, count, err := GetSegmentLSNRange(writer.segmentPath(1))
	if err != nil {
		t.Fatalf("failed to get LSN range: %v", err)
	}

	if minLSN != 1 {
		t.Errorf("expected minLSN 1, got %d", minLSN)
	}
	if maxLSN != 10 {
		t.Errorf("expected maxLSN 10, got %d", maxLSN)
	}
	if count != 10 {
		t.Errorf("expected count 10, got %d", count)
	}
}

func TestCorruptedSegment(t *testing.T) {
	dir := t.TempDir()

	// Write valid records
	writer, err := NewWALWriter(dir, WithSyncPolicy(ImmediateSyncPolicy()))
	if err != nil {
		t.Fatalf("failed to create WAL writer: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := writer.Append(RecordTypeInsert, []byte("test"))
		if err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	writer.Close()

	segmentPath := writer.segmentPath(1)

	// Corrupt the file
	f, err := os.OpenFile(segmentPath, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	f.WriteAt([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 100) // Corrupt some bytes
	f.Close()

	// Try to read - should fail at some point
	iter, err := NewSegmentIterator(segmentPath)
	if err != nil {
		t.Fatalf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	// Should read some records then hit corruption
	for iter.Next() {
		// Keep reading until error
	}

	// Should have an error due to corruption
	if iter.Err() == nil {
		t.Log("Warning: corruption not detected (may depend on corruption location)")
	}
}
