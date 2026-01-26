package wal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSegmentFilename(t *testing.T) {
	tests := []struct {
		id       uint64
		expected string
	}{
		{1, "wal_000000000001.seg"},
		{100, "wal_000000000100.seg"},
		{999999999999, "wal_999999999999.seg"},
	}

	for _, tc := range tests {
		got := SegmentFilename(tc.id)
		if got != tc.expected {
			t.Errorf("SegmentFilename(%d) = %s, want %s", tc.id, got, tc.expected)
		}
	}
}

func TestCompactedSegmentFilename(t *testing.T) {
	tests := []struct {
		id       uint64
		expected string
	}{
		{1, "cmp_000000000001.seg"},
		{100, "cmp_000000000100.seg"},
		{999999999999, "cmp_999999999999.seg"},
	}

	for _, tc := range tests {
		got := CompactedSegmentFilename(tc.id)
		if got != tc.expected {
			t.Errorf("CompactedSegmentFilename(%d) = %s, want %s", tc.id, got, tc.expected)
		}
	}
}

func TestGetSegmentID(t *testing.T) {
	tests := []struct {
		filename string
		expected uint64
		wantErr  bool
	}{
		{"wal_000000000001.seg", 1, false},
		{"wal_000000000100.seg", 100, false},
		{"/path/to/wal_000000000005.seg", 5, false},
		{"cmp_000000000001.seg", 1, false},
		{"cmp_000000000100.seg", 100, false},
		{"/path/to/cmp_000000000005.seg", 5, false},
		{"invalid.seg", 0, true},
		{"wal_abc.seg", 0, true},
	}

	for _, tc := range tests {
		got, err := GetSegmentID(tc.filename)
		if tc.wantErr {
			if err == nil {
				t.Errorf("GetSegmentID(%s) should have errored", tc.filename)
			}
			continue
		}
		if err != nil {
			t.Errorf("GetSegmentID(%s) unexpected error: %v", tc.filename, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("GetSegmentID(%s) = %d, want %d", tc.filename, got, tc.expected)
		}
	}
}

func TestListSegmentFiles(t *testing.T) {
	dir := t.TempDir()

	// Create mixed WAL and compacted segment files
	files := []string{
		"wal_000000000001.seg",
		"wal_000000000003.seg",
		"cmp_000000000002.seg", // Compacted segment between WAL segments
		"cmp_000000000004.seg",
		"wal_000000000005.seg",
		"other.txt", // Should be ignored
	}

	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	segments, err := ListSegmentFiles(dir)
	if err != nil {
		t.Fatalf("ListSegmentFiles failed: %v", err)
	}

	// Should have 5 segment files (excluding other.txt)
	if len(segments) != 5 {
		t.Errorf("expected 5 segments, got %d", len(segments))
	}

	// Should be sorted by segment ID
	expectedOrder := []uint64{1, 2, 3, 4, 5}
	for i, seg := range segments {
		id, _ := GetSegmentID(seg)
		if id != expectedOrder[i] {
			t.Errorf("segment %d: expected ID %d, got %d", i, expectedOrder[i], id)
		}
	}
}

func TestCompactedAndWALSegmentsNoCollision(t *testing.T) {
	// This test verifies that compacted segments (cmp_) and WAL segments (wal_)
	// can have the same ID without collision because they use different prefixes

	dir := t.TempDir()

	// Create both a WAL segment and compacted segment with ID 5
	walPath := filepath.Join(dir, SegmentFilename(5))
	cmpPath := filepath.Join(dir, CompactedSegmentFilename(5))

	if err := os.WriteFile(walPath, []byte("wal data"), 0644); err != nil {
		t.Fatalf("failed to create WAL segment: %v", err)
	}
	if err := os.WriteFile(cmpPath, []byte("compacted data"), 0644); err != nil {
		t.Fatalf("failed to create compacted segment: %v", err)
	}

	// Both files should exist
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Error("WAL segment file should exist")
	}
	if _, err := os.Stat(cmpPath); os.IsNotExist(err) {
		t.Error("Compacted segment file should exist")
	}

	// ListSegmentFiles should find both
	segments, err := ListSegmentFiles(dir)
	if err != nil {
		t.Fatalf("ListSegmentFiles failed: %v", err)
	}

	if len(segments) != 2 {
		t.Errorf("expected 2 segments, got %d", len(segments))
	}

	// Verify both are included
	foundWAL := false
	foundCMP := false
	for _, seg := range segments {
		base := filepath.Base(seg)
		if base == "wal_000000000005.seg" {
			foundWAL = true
		}
		if base == "cmp_000000000005.seg" {
			foundCMP = true
		}
	}

	if !foundWAL {
		t.Error("WAL segment not found in list")
	}
	if !foundCMP {
		t.Error("Compacted segment not found in list")
	}
}
