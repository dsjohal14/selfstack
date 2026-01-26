package wal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// SegmentIterator iterates over records in a WAL segment file
type SegmentIterator struct {
	file     *os.File
	filePath string
	offset   int64
	record   *Record
	err      error
	fromLSN  uint64 // Skip records before this LSN (0 = read all)
}

// NewSegmentIterator creates an iterator for the given segment file
func NewSegmentIterator(filePath string) (*SegmentIterator, error) {
	return NewSegmentIteratorFromLSN(filePath, 0)
}

// NewSegmentIteratorFromLSN creates an iterator that skips records before the given LSN
func NewSegmentIteratorFromLSN(filePath string, fromLSN uint64) (*SegmentIterator, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open segment %s: %w", filePath, err)
	}

	return &SegmentIterator{
		file:     f,
		filePath: filePath,
		offset:   0,
		fromLSN:  fromLSN,
	}, nil
}

// Next advances to the next record. Returns false when done or on error.
func (it *SegmentIterator) Next() bool {
	for {
		// Read header
		header := make([]byte, HeaderSize)
		n, err := io.ReadFull(it.file, header)
		if err != nil {
			if err == io.EOF {
				return false // Normal end
			}
			it.err = fmt.Errorf("failed to read header at offset %d: %w", it.offset, err)
			return false
		}
		if n < HeaderSize {
			it.err = fmt.Errorf("short header read at offset %d: %d < %d", it.offset, n, HeaderSize)
			return false
		}

		// Parse header fields
		magic := binary.LittleEndian.Uint32(header[0:4])
		if magic != MagicBytes {
			it.err = fmt.Errorf("invalid magic at offset %d: expected 0x%X, got 0x%X", it.offset, MagicBytes, magic)
			return false
		}

		recType := RecordType(header[4])
		flags := RecordFlags(header[5])
		reserved := binary.LittleEndian.Uint16(header[6:8])
		lsn := binary.LittleEndian.Uint64(header[8:16])
		payloadLen := binary.LittleEndian.Uint32(header[16:20])
		headerCRC := binary.LittleEndian.Uint32(header[20:24])

		// Verify header CRC
		expectedHeaderCRC := crc32.ChecksumIEEE(header[0:20])
		if headerCRC != expectedHeaderCRC {
			it.err = fmt.Errorf("header CRC mismatch at offset %d: expected 0x%X, got 0x%X", it.offset, expectedHeaderCRC, headerCRC)
			return false
		}

		// Sanity check payload length
		if payloadLen > MaxPayloadSize {
			it.err = fmt.Errorf("payload too large at offset %d: %d > %d", it.offset, payloadLen, MaxPayloadSize)
			return false
		}

		// Read payload
		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			n, err = io.ReadFull(it.file, payload)
			if err != nil {
				it.err = fmt.Errorf("failed to read payload at offset %d: %w", it.offset, err)
				return false
			}
			if uint32(n) < payloadLen {
				it.err = fmt.Errorf("short payload read at offset %d: %d < %d", it.offset, n, payloadLen)
				return false
			}
		}

		// Read payload CRC
		payloadCRCBuf := make([]byte, 4)
		_, err = io.ReadFull(it.file, payloadCRCBuf)
		if err != nil {
			it.err = fmt.Errorf("failed to read payload CRC at offset %d: %w", it.offset, err)
			return false
		}
		payloadCRC := binary.LittleEndian.Uint32(payloadCRCBuf)

		// Verify payload CRC
		expectedPayloadCRC := crc32.ChecksumIEEE(payload)
		if payloadCRC != expectedPayloadCRC {
			it.err = fmt.Errorf("payload CRC mismatch at offset %d: expected 0x%X, got 0x%X", it.offset, expectedPayloadCRC, payloadCRC)
			return false
		}

		// Build record
		it.record = &Record{
			Magic:      magic,
			Type:       recType,
			Flags:      flags,
			Reserved:   reserved,
			LSN:        lsn,
			PayloadLen: payloadLen,
			HeaderCRC:  headerCRC,
			Payload:    payload,
			PayloadCRC: payloadCRC,
		}

		// Update offset
		it.offset += int64(HeaderSize + payloadLen + 4)

		// Skip if before fromLSN
		if it.fromLSN > 0 && lsn < it.fromLSN {
			continue
		}

		return true
	}
}

// Record returns the current record
func (it *SegmentIterator) Record() *Record {
	return it.record
}

// Err returns any error that occurred during iteration
func (it *SegmentIterator) Err() error {
	return it.err
}

// Offset returns the current byte offset in the file
func (it *SegmentIterator) Offset() int64 {
	return it.offset
}

// Close closes the iterator
func (it *SegmentIterator) Close() error {
	if it.file != nil {
		return it.file.Close()
	}
	return nil
}

// CalculateSegmentChecksum calculates the CRC32 checksum of an entire segment file
func CalculateSegmentChecksum(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open segment: %w", err)
	}
	defer func() { _ = f.Close() }()

	hash := crc32.NewIEEE()
	if _, err := io.Copy(hash, f); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return fmt.Sprintf("%08x", hash.Sum32()), nil
}

// VerifySegmentChecksum verifies a segment file against an expected checksum
func VerifySegmentChecksum(filePath, expectedChecksum string) (bool, error) {
	actual, err := CalculateSegmentChecksum(filePath)
	if err != nil {
		return false, err
	}
	return actual == expectedChecksum, nil
}

// ReadAllRecords reads all records from a segment file
func ReadAllRecords(filePath string) ([]*Record, error) {
	return ReadRecordsFromLSN(filePath, 0)
}

// ReadRecordsFromLSN reads records from a segment file starting from a given LSN
func ReadRecordsFromLSN(filePath string, fromLSN uint64) ([]*Record, error) {
	iter, err := NewSegmentIteratorFromLSN(filePath, fromLSN)
	if err != nil {
		return nil, err
	}
	defer func() { _ = iter.Close() }()

	var records []*Record
	for iter.Next() {
		rec := iter.Record()
		// Make a copy to avoid reference issues
		recCopy := *rec
		recCopy.Payload = make([]byte, len(rec.Payload))
		copy(recCopy.Payload, rec.Payload)
		records = append(records, &recCopy)
	}

	if err := iter.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// GetSegmentLSNRange returns the min and max LSN in a segment
func GetSegmentLSNRange(filePath string) (minLSN, maxLSN uint64, count int, err error) {
	iter, err := NewSegmentIterator(filePath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = iter.Close() }()

	first := true
	for iter.Next() {
		lsn := iter.Record().LSN
		if first {
			minLSN = lsn
			maxLSN = lsn
			first = false
		}
		if lsn < minLSN {
			minLSN = lsn
		}
		if lsn > maxLSN {
			maxLSN = lsn
		}
		count++
	}

	if err := iter.Err(); err != nil {
		return 0, 0, 0, err
	}

	return minLSN, maxLSN, count, nil
}

// SegmentWriter writes records to a segment file
type SegmentWriter struct {
	file     *os.File
	filePath string
	offset   int64
	checksum uint32
}

// NewSegmentWriter creates a new segment writer
func NewSegmentWriter(filePath string) (*SegmentWriter, error) {
	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create segment %s: %w", filePath, err)
	}

	return &SegmentWriter{
		file:     f,
		filePath: filePath,
		offset:   0,
		checksum: 0,
	}, nil
}

// Write writes a record to the segment
func (sw *SegmentWriter) Write(rec *Record) error {
	data := rec.Encode()

	n, err := sw.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("short write: %d < %d", n, len(data))
	}

	sw.offset += int64(n)
	sw.checksum = crc32.Update(sw.checksum, crc32.IEEETable, data)
	return nil
}

// Finalize syncs and returns the segment checksum
func (sw *SegmentWriter) Finalize() (string, error) {
	if err := sw.file.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync segment: %w", err)
	}
	return fmt.Sprintf("%08x", sw.checksum), nil
}

// Close closes the segment writer
func (sw *SegmentWriter) Close() error {
	if sw.file != nil {
		return sw.file.Close()
	}
	return nil
}

// Offset returns the current byte offset
func (sw *SegmentWriter) Offset() int64 {
	return sw.offset
}

// FilePath returns the segment file path
func (sw *SegmentWriter) FilePath() string {
	return sw.filePath
}
