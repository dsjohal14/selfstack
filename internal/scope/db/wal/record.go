// Package wal implements a Write-Ahead Log with LSN tracking and CRC32 checksums.
package wal

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
)

// WAL Record Format (24-byte header + payload):
// ┌─────────────────────────────────────────────────────────────┐
// │ Magic (4B)  │ Type (1B) │ Flags (1B) │ Reserved (2B)        │
// ├─────────────────────────────────────────────────────────────┤
// │ LSN (8B, uint64) - Log Sequence Number                      │
// ├─────────────────────────────────────────────────────────────┤
// │ PayloadLen (4B, uint32)                                     │
// ├─────────────────────────────────────────────────────────────┤
// │ HeaderCRC32 (4B) - checksum of bytes [0:20]                 │
// ├─────────────────────────────────────────────────────────────┤
// │ Payload (variable) - Document data                          │
// ├─────────────────────────────────────────────────────────────┤
// │ PayloadCRC32 (4B) - checksum of payload                     │
// └─────────────────────────────────────────────────────────────┘

const (
	// Magic bytes for WAL record identification
	MagicBytes uint32 = 0x57414C52 // "WALR"

	// HeaderSize is the fixed size of the record header
	HeaderSize = 24

	// EmbeddingSize is the fixed size of a 128-dim float32 embedding
	EmbeddingSize = relay.EmbeddingDim * 4 // 512 bytes

	// MaxPayloadSize limits individual record size (10MB)
	MaxPayloadSize = 10 * 1024 * 1024

	// MaxDocIDLen limits document ID length
	MaxDocIDLen = 65535 // uint16 max
)

// RecordType identifies the type of WAL record
type RecordType uint8

const (
	RecordTypeInsert     RecordType = 0x01 // New document
	RecordTypeUpdate     RecordType = 0x02 // Replace existing doc
	RecordTypeDelete     RecordType = 0x03 // Tombstone marker
	RecordTypeCheckpoint RecordType = 0x04 // Marks flushed position
)

func (r RecordType) String() string {
	switch r {
	case RecordTypeInsert:
		return "INSERT"
	case RecordTypeUpdate:
		return "UPDATE"
	case RecordTypeDelete:
		return "DELETE"
	case RecordTypeCheckpoint:
		return "CHECKPOINT"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", r)
	}
}

// RecordFlags holds optional flags for records
type RecordFlags uint8

const (
	FlagNone       RecordFlags = 0x00
	FlagCompressed RecordFlags = 0x01 // Payload is compressed (future use)
)

// Record represents a WAL record with header and payload
type Record struct {
	Magic      uint32
	Type       RecordType
	Flags      RecordFlags
	Reserved   uint16
	LSN        uint64
	PayloadLen uint32
	HeaderCRC  uint32
	Payload    []byte
	PayloadCRC uint32
}

// DocPayload represents the payload for INSERT/UPDATE records
type DocPayload struct {
	DocID     string          `json:"-"` // Stored separately in binary
	Metadata  DocMetadata     `json:"metadata"`
	Embedding relay.Embedding `json:"-"` // Stored as raw bytes after JSON
}

// DocMetadata contains document metadata stored as JSON
type DocMetadata struct {
	Source    string            `json:"source"`
	Title     string            `json:"title"`
	Text      string            `json:"text"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// NewRecord creates a new WAL record with the given type and payload
func NewRecord(recType RecordType, lsn uint64, payload []byte) (*Record, error) {
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d > %d", len(payload), MaxPayloadSize)
	}

	rec := &Record{
		Magic:      MagicBytes,
		Type:       recType,
		Flags:      FlagNone,
		Reserved:   0,
		LSN:        lsn,
		PayloadLen: uint32(len(payload)),
		Payload:    payload,
	}

	// Calculate header CRC (first 20 bytes before HeaderCRC field)
	rec.HeaderCRC = rec.calculateHeaderCRC()

	// Calculate payload CRC
	rec.PayloadCRC = crc32.ChecksumIEEE(payload)

	return rec, nil
}

// calculateHeaderCRC computes CRC32 of header bytes [0:20]
func (r *Record) calculateHeaderCRC() uint32 {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:4], r.Magic)
	buf[4] = byte(r.Type)
	buf[5] = byte(r.Flags)
	binary.LittleEndian.PutUint16(buf[6:8], r.Reserved)
	binary.LittleEndian.PutUint64(buf[8:16], r.LSN)
	binary.LittleEndian.PutUint32(buf[16:20], r.PayloadLen)
	return crc32.ChecksumIEEE(buf)
}

// Encode serializes the record to bytes
func (r *Record) Encode() []byte {
	buf := make([]byte, HeaderSize+len(r.Payload)+4) // header + payload + payload CRC

	// Header
	binary.LittleEndian.PutUint32(buf[0:4], r.Magic)
	buf[4] = byte(r.Type)
	buf[5] = byte(r.Flags)
	binary.LittleEndian.PutUint16(buf[6:8], r.Reserved)
	binary.LittleEndian.PutUint64(buf[8:16], r.LSN)
	binary.LittleEndian.PutUint32(buf[16:20], r.PayloadLen)
	binary.LittleEndian.PutUint32(buf[20:24], r.HeaderCRC)

	// Payload
	copy(buf[HeaderSize:], r.Payload)

	// Payload CRC
	binary.LittleEndian.PutUint32(buf[HeaderSize+len(r.Payload):], r.PayloadCRC)

	return buf
}

// DecodeRecord deserializes a record from bytes
func DecodeRecord(data []byte) (*Record, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("data too short for header: %d < %d", len(data), HeaderSize)
	}

	rec := &Record{
		Magic:      binary.LittleEndian.Uint32(data[0:4]),
		Type:       RecordType(data[4]),
		Flags:      RecordFlags(data[5]),
		Reserved:   binary.LittleEndian.Uint16(data[6:8]),
		LSN:        binary.LittleEndian.Uint64(data[8:16]),
		PayloadLen: binary.LittleEndian.Uint32(data[16:20]),
		HeaderCRC:  binary.LittleEndian.Uint32(data[20:24]),
	}

	// Verify magic
	if rec.Magic != MagicBytes {
		return nil, fmt.Errorf("invalid magic: expected 0x%X, got 0x%X", MagicBytes, rec.Magic)
	}

	// Verify header CRC
	expectedHeaderCRC := rec.calculateHeaderCRC()
	if rec.HeaderCRC != expectedHeaderCRC {
		return nil, fmt.Errorf("header CRC mismatch: expected 0x%X, got 0x%X", expectedHeaderCRC, rec.HeaderCRC)
	}

	// Check payload length
	totalLen := HeaderSize + int(rec.PayloadLen) + 4 // +4 for payload CRC
	if len(data) < totalLen {
		return nil, fmt.Errorf("data too short for payload: %d < %d", len(data), totalLen)
	}

	// Extract payload
	rec.Payload = make([]byte, rec.PayloadLen)
	copy(rec.Payload, data[HeaderSize:HeaderSize+rec.PayloadLen])

	// Extract and verify payload CRC
	rec.PayloadCRC = binary.LittleEndian.Uint32(data[HeaderSize+rec.PayloadLen : totalLen])
	expectedPayloadCRC := crc32.ChecksumIEEE(rec.Payload)
	if rec.PayloadCRC != expectedPayloadCRC {
		return nil, fmt.Errorf("payload CRC mismatch: expected 0x%X, got 0x%X", expectedPayloadCRC, rec.PayloadCRC)
	}

	return rec, nil
}

// TotalSize returns the total size of the encoded record
func (r *Record) TotalSize() int {
	return HeaderSize + int(r.PayloadLen) + 4
}

// VerifyChecksums validates both header and payload CRCs
func (r *Record) VerifyChecksums() error {
	// Verify header CRC
	expectedHeaderCRC := r.calculateHeaderCRC()
	if r.HeaderCRC != expectedHeaderCRC {
		return fmt.Errorf("header CRC mismatch: expected 0x%X, got 0x%X", expectedHeaderCRC, r.HeaderCRC)
	}

	// Verify payload CRC
	expectedPayloadCRC := crc32.ChecksumIEEE(r.Payload)
	if r.PayloadCRC != expectedPayloadCRC {
		return fmt.Errorf("payload CRC mismatch: expected 0x%X, got 0x%X", expectedPayloadCRC, r.PayloadCRC)
	}

	return nil
}

// EncodeDocPayload serializes a document payload for INSERT/UPDATE records
// Format:
// - DocID Length (2B) + DocID (variable)
// - Metadata Length (4B) + JSON (variable)
// - Embedding (512B = 128 * float32)
func EncodeDocPayload(docID string, meta DocMetadata, embedding relay.Embedding) ([]byte, error) {
	if len(docID) > MaxDocIDLen {
		return nil, fmt.Errorf("docID too long: %d > %d", len(docID), MaxDocIDLen)
	}

	// Encode metadata as JSON
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Calculate total size
	totalSize := 2 + len(docID) + 4 + len(metaJSON) + EmbeddingSize
	buf := bytes.NewBuffer(make([]byte, 0, totalSize))

	// DocID
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(docID))); err != nil {
		return nil, err
	}
	buf.WriteString(docID)

	// Metadata JSON
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(metaJSON))); err != nil {
		return nil, err
	}
	buf.Write(metaJSON)

	// Embedding (raw float32 bytes)
	if err := binary.Write(buf, binary.LittleEndian, embedding); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// DecodeDocPayload deserializes a document payload from INSERT/UPDATE records
func DecodeDocPayload(data []byte) (string, DocMetadata, relay.Embedding, error) {
	var meta DocMetadata
	var embedding relay.Embedding

	if len(data) < 6 { // minimum: 2 (docID len) + 4 (meta len)
		return "", meta, embedding, fmt.Errorf("payload too short: %d", len(data))
	}

	buf := bytes.NewReader(data)

	// Read DocID
	var docIDLen uint16
	if err := binary.Read(buf, binary.LittleEndian, &docIDLen); err != nil {
		return "", meta, embedding, fmt.Errorf("failed to read docID length: %w", err)
	}

	docIDBytes := make([]byte, docIDLen)
	if _, err := buf.Read(docIDBytes); err != nil {
		return "", meta, embedding, fmt.Errorf("failed to read docID: %w", err)
	}
	docID := string(docIDBytes)

	// Read metadata JSON
	var metaLen uint32
	if err := binary.Read(buf, binary.LittleEndian, &metaLen); err != nil {
		return "", meta, embedding, fmt.Errorf("failed to read metadata length: %w", err)
	}

	metaJSON := make([]byte, metaLen)
	if _, err := buf.Read(metaJSON); err != nil {
		return "", meta, embedding, fmt.Errorf("failed to read metadata: %w", err)
	}

	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return "", meta, embedding, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	// Read embedding
	if err := binary.Read(buf, binary.LittleEndian, &embedding); err != nil {
		return "", meta, embedding, fmt.Errorf("failed to read embedding: %w", err)
	}

	return docID, meta, embedding, nil
}

// EncodeDeletePayload serializes a delete payload (just the DocID)
func EncodeDeletePayload(docID string) ([]byte, error) {
	if len(docID) > MaxDocIDLen {
		return nil, fmt.Errorf("docID too long: %d > %d", len(docID), MaxDocIDLen)
	}

	buf := bytes.NewBuffer(make([]byte, 0, 2+len(docID)))
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(docID))); err != nil {
		return nil, err
	}
	buf.WriteString(docID)
	return buf.Bytes(), nil
}

// DecodeDeletePayload deserializes a delete payload
func DecodeDeletePayload(data []byte) (string, error) {
	if len(data) < 2 {
		return "", fmt.Errorf("delete payload too short: %d", len(data))
	}

	buf := bytes.NewReader(data)
	var docIDLen uint16
	if err := binary.Read(buf, binary.LittleEndian, &docIDLen); err != nil {
		return "", fmt.Errorf("failed to read docID length: %w", err)
	}

	docIDBytes := make([]byte, docIDLen)
	if _, err := buf.Read(docIDBytes); err != nil {
		return "", fmt.Errorf("failed to read docID: %w", err)
	}

	return string(docIDBytes), nil
}

// EncodeCheckpointPayload serializes a checkpoint payload
func EncodeCheckpointPayload(checkpointLSN uint64) ([]byte, error) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, checkpointLSN)
	return buf, nil
}

// DecodeCheckpointPayload deserializes a checkpoint payload
func DecodeCheckpointPayload(data []byte) (uint64, error) {
	if len(data) < 8 {
		return 0, fmt.Errorf("checkpoint payload too short: %d", len(data))
	}
	return binary.LittleEndian.Uint64(data), nil
}
