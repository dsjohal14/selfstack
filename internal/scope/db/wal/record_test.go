package wal

import (
	"testing"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
)

func TestNewRecord(t *testing.T) {
	payload := []byte("test payload")
	rec, err := NewRecord(RecordTypeInsert, 1, payload)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	if rec.Magic != MagicBytes {
		t.Errorf("magic mismatch: expected 0x%X, got 0x%X", MagicBytes, rec.Magic)
	}
	if rec.Type != RecordTypeInsert {
		t.Errorf("type mismatch: expected %v, got %v", RecordTypeInsert, rec.Type)
	}
	if rec.LSN != 1 {
		t.Errorf("LSN mismatch: expected 1, got %d", rec.LSN)
	}
	if rec.PayloadLen != uint32(len(payload)) {
		t.Errorf("payload length mismatch: expected %d, got %d", len(payload), rec.PayloadLen)
	}
}

func TestRecordEncodeDeocde(t *testing.T) {
	payload := []byte("test payload data")
	original, err := NewRecord(RecordTypeUpdate, 42, payload)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	// Encode
	encoded := original.Encode()

	// Decode
	decoded, err := DecodeRecord(encoded)
	if err != nil {
		t.Fatalf("failed to decode record: %v", err)
	}

	// Verify
	if decoded.Magic != original.Magic {
		t.Errorf("magic mismatch: expected 0x%X, got 0x%X", original.Magic, decoded.Magic)
	}
	if decoded.Type != original.Type {
		t.Errorf("type mismatch: expected %v, got %v", original.Type, decoded.Type)
	}
	if decoded.LSN != original.LSN {
		t.Errorf("LSN mismatch: expected %d, got %d", original.LSN, decoded.LSN)
	}
	if decoded.PayloadLen != original.PayloadLen {
		t.Errorf("payload length mismatch: expected %d, got %d", original.PayloadLen, decoded.PayloadLen)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("payload mismatch: expected %q, got %q", original.Payload, decoded.Payload)
	}
}

func TestRecordCRCValidation(t *testing.T) {
	payload := []byte("test payload")
	rec, err := NewRecord(RecordTypeInsert, 1, payload)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	encoded := rec.Encode()

	// Corrupt header
	corruptedHeader := make([]byte, len(encoded))
	copy(corruptedHeader, encoded)
	corruptedHeader[10] ^= 0xFF // Flip bits in LSN field

	_, err = DecodeRecord(corruptedHeader)
	if err == nil {
		t.Error("expected error for corrupted header, got nil")
	}

	// Corrupt payload
	corruptedPayload := make([]byte, len(encoded))
	copy(corruptedPayload, encoded)
	corruptedPayload[HeaderSize+5] ^= 0xFF // Flip bits in payload

	_, err = DecodeRecord(corruptedPayload)
	if err == nil {
		t.Error("expected error for corrupted payload, got nil")
	}
}

func TestDocPayloadEncodeDecode(t *testing.T) {
	docID := "test-doc-123"
	meta := DocMetadata{
		Source:    "test-source",
		Title:     "Test Document",
		Text:      "This is the document content for testing.",
		Metadata:  map[string]string{"key": "value"},
		CreatedAt: time.Now().Truncate(time.Millisecond),
	}
	embedding := relay.DeterministicEmbed("test text")

	// Encode
	payload, err := EncodeDocPayload(docID, meta, embedding)
	if err != nil {
		t.Fatalf("failed to encode doc payload: %v", err)
	}

	// Decode
	decodedID, decodedMeta, decodedEmb, err := DecodeDocPayload(payload)
	if err != nil {
		t.Fatalf("failed to decode doc payload: %v", err)
	}

	// Verify
	if decodedID != docID {
		t.Errorf("docID mismatch: expected %q, got %q", docID, decodedID)
	}
	if decodedMeta.Source != meta.Source {
		t.Errorf("source mismatch: expected %q, got %q", meta.Source, decodedMeta.Source)
	}
	if decodedMeta.Title != meta.Title {
		t.Errorf("title mismatch: expected %q, got %q", meta.Title, decodedMeta.Title)
	}
	if decodedMeta.Text != meta.Text {
		t.Errorf("text mismatch: expected %q, got %q", meta.Text, decodedMeta.Text)
	}
	if decodedMeta.Metadata["key"] != meta.Metadata["key"] {
		t.Errorf("metadata mismatch: expected %v, got %v", meta.Metadata, decodedMeta.Metadata)
	}
	if !decodedMeta.CreatedAt.Equal(meta.CreatedAt) {
		t.Errorf("createdAt mismatch: expected %v, got %v", meta.CreatedAt, decodedMeta.CreatedAt)
	}

	// Verify embedding
	for i := 0; i < relay.EmbeddingDim; i++ {
		if decodedEmb[i] != embedding[i] {
			t.Errorf("embedding[%d] mismatch: expected %f, got %f", i, embedding[i], decodedEmb[i])
			break
		}
	}
}

func TestDeletePayloadEncodeDecode(t *testing.T) {
	docID := "doc-to-delete-456"

	// Encode
	payload, err := EncodeDeletePayload(docID)
	if err != nil {
		t.Fatalf("failed to encode delete payload: %v", err)
	}

	// Decode
	decodedID, err := DecodeDeletePayload(payload)
	if err != nil {
		t.Fatalf("failed to decode delete payload: %v", err)
	}

	if decodedID != docID {
		t.Errorf("docID mismatch: expected %q, got %q", docID, decodedID)
	}
}

func TestCheckpointPayloadEncodeDecode(t *testing.T) {
	checkpointLSN := uint64(12345)

	// Encode
	payload, err := EncodeCheckpointPayload(checkpointLSN)
	if err != nil {
		t.Fatalf("failed to encode checkpoint payload: %v", err)
	}

	// Decode
	decodedLSN, err := DecodeCheckpointPayload(payload)
	if err != nil {
		t.Fatalf("failed to decode checkpoint payload: %v", err)
	}

	if decodedLSN != checkpointLSN {
		t.Errorf("checkpoint LSN mismatch: expected %d, got %d", checkpointLSN, decodedLSN)
	}
}

func TestRecordTypeString(t *testing.T) {
	tests := []struct {
		recType RecordType
		want    string
	}{
		{RecordTypeInsert, "INSERT"},
		{RecordTypeUpdate, "UPDATE"},
		{RecordTypeDelete, "DELETE"},
		{RecordTypeCheckpoint, "CHECKPOINT"},
		{RecordType(99), "UNKNOWN(99)"},
	}

	for _, tt := range tests {
		got := tt.recType.String()
		if got != tt.want {
			t.Errorf("RecordType(%d).String() = %q, want %q", tt.recType, got, tt.want)
		}
	}
}

func TestRecordTotalSize(t *testing.T) {
	payload := []byte("test payload")
	rec, err := NewRecord(RecordTypeInsert, 1, payload)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	expectedSize := HeaderSize + len(payload) + 4 // header + payload + payload CRC
	if rec.TotalSize() != expectedSize {
		t.Errorf("TotalSize() = %d, want %d", rec.TotalSize(), expectedSize)
	}
}

func TestRecordVerifyChecksums(t *testing.T) {
	payload := []byte("test payload")
	rec, err := NewRecord(RecordTypeInsert, 1, payload)
	if err != nil {
		t.Fatalf("failed to create record: %v", err)
	}

	if err := rec.VerifyChecksums(); err != nil {
		t.Errorf("VerifyChecksums() failed for valid record: %v", err)
	}

	// Corrupt header CRC
	rec.HeaderCRC ^= 0xFFFFFFFF
	if err := rec.VerifyChecksums(); err == nil {
		t.Error("VerifyChecksums() should fail for corrupted header CRC")
	}
	rec.HeaderCRC ^= 0xFFFFFFFF // Restore

	// Corrupt payload CRC
	rec.PayloadCRC ^= 0xFFFFFFFF
	if err := rec.VerifyChecksums(); err == nil {
		t.Error("VerifyChecksums() should fail for corrupted payload CRC")
	}
}
