package wal

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultMaxSegmentSize is the default max size before rotation (64MB)
const DefaultMaxSegmentSize = 64 * 1024 * 1024

// SyncPolicy controls when to fsync writes to disk
type SyncPolicy struct {
	Immediate bool          // Sync after every write
	Interval  time.Duration // Sync every N ms (default: 100ms)
	BatchSize int           // Sync every N records (default: 100)
}

// DefaultSyncPolicy returns a balanced sync policy
func DefaultSyncPolicy() SyncPolicy {
	return SyncPolicy{
		Immediate: false,
		Interval:  100 * time.Millisecond,
		BatchSize: 100,
	}
}

// ImmediateSyncPolicy returns a policy that syncs after every write
func ImmediateSyncPolicy() SyncPolicy {
	return SyncPolicy{
		Immediate: true,
	}
}

// WALWriter is a thread-safe Write-Ahead Log writer
type WALWriter struct {
	mu         sync.Mutex    // Serialize all writes
	dir        string        // WAL directory
	file       *os.File      // Current segment file
	segmentID  uint64        // Current segment number
	lsn        uint64        // Next LSN to assign (atomic)
	offset     int64         // Current file offset
	syncPolicy SyncPolicy    // When to fsync
	maxSize    int64         // Max segment size
	manifest   ManifestStore // Postgres manifest (optional)

	// Sync tracking
	pendingWrites int       // Number of writes since last sync
	lastSync      time.Time // Time of last sync
	syncTicker    *time.Ticker
	stopSync      chan struct{}
	wg            sync.WaitGroup

	closed bool
}

// WALWriterOption configures a WALWriter
type WALWriterOption func(*WALWriter)

// WithSyncPolicy sets the sync policy
func WithSyncPolicy(policy SyncPolicy) WALWriterOption {
	return func(w *WALWriter) {
		w.syncPolicy = policy
	}
}

// WithMaxSegmentSize sets the max segment size
func WithMaxSegmentSize(size int64) WALWriterOption {
	return func(w *WALWriter) {
		w.maxSize = size
	}
}

// WithManifest sets the Postgres manifest store
func WithManifest(manifest ManifestStore) WALWriterOption {
	return func(w *WALWriter) {
		w.manifest = manifest
	}
}

// WithInitialLSN sets the initial LSN (for recovery)
func WithInitialLSN(lsn uint64) WALWriterOption {
	return func(w *WALWriter) {
		w.lsn = lsn
	}
}

// WithInitialSegmentID sets the initial segment ID (for recovery)
func WithInitialSegmentID(segmentID uint64) WALWriterOption {
	return func(w *WALWriter) {
		w.segmentID = segmentID
	}
}

// NewWALWriter creates a new WAL writer
func NewWALWriter(dir string, opts ...WALWriterOption) (*WALWriter, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	w := &WALWriter{
		dir:        dir,
		segmentID:  1,
		lsn:        1,
		offset:     0,
		syncPolicy: DefaultSyncPolicy(),
		maxSize:    DefaultMaxSegmentSize,
		lastSync:   time.Now(),
		stopSync:   make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(w)
	}

	// Open initial segment
	if err := w.openSegment(); err != nil {
		return nil, err
	}

	// Start background sync if not immediate
	if !w.syncPolicy.Immediate && w.syncPolicy.Interval > 0 {
		w.startBackgroundSync()
	}

	return w, nil
}

// openSegment opens a new segment file, truncating any corrupt tail
func (w *WALWriter) openSegment() error {
	path := w.segmentPath(w.segmentID)

	// Check if file exists and has content - need to verify/truncate corrupt tail
	if stat, err := os.Stat(path); err == nil && stat.Size() > 0 {
		validOffset, err := w.findLastValidOffset(path)
		if err != nil {
			return fmt.Errorf("failed to scan segment for corruption: %w", err)
		}

		// Truncate at last valid record if file has corrupt tail
		if validOffset < stat.Size() {
			fmt.Printf("truncating corrupt tail in segment %s: %d -> %d bytes\n",
				path, stat.Size(), validOffset)
			if err := os.Truncate(path, validOffset); err != nil {
				return fmt.Errorf("failed to truncate corrupt segment: %w", err)
			}
		}
	}

	// Open for append
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open segment %s: %w", path, err)
	}

	// Get current file size
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to stat segment %s: %w", path, err)
	}

	w.file = f
	w.offset = stat.Size()
	return nil
}

// findLastValidOffset scans a segment and returns the offset after the last valid record
func (w *WALWriter) findLastValidOffset(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	var lastValidOffset int64 = 0
	var offset int64 = 0

	for {
		// Read header using io.ReadFull to handle short reads correctly
		header := make([]byte, HeaderSize)
		_, err := io.ReadFull(f, header)
		if err != nil {
			break // EOF or incomplete header
		}

		// Verify magic
		magic := binary.LittleEndian.Uint32(header[0:4])
		if magic != MagicBytes {
			break // Corrupt record
		}

		// Get payload length
		payloadLen := binary.LittleEndian.Uint32(header[16:20])
		if payloadLen > MaxPayloadSize {
			break // Invalid payload size
		}

		// Verify header CRC before reading payload
		headerCRC := binary.LittleEndian.Uint32(header[20:24])
		expectedHeaderCRC := crc32.ChecksumIEEE(header[0:20])
		if headerCRC != expectedHeaderCRC {
			break // Corrupt header
		}

		// Read payload + CRC using io.ReadFull
		payloadAndCRC := make([]byte, payloadLen+4)
		_, err = io.ReadFull(f, payloadAndCRC)
		if err != nil {
			break // Incomplete record
		}

		// Verify payload CRC
		payloadCRC := binary.LittleEndian.Uint32(payloadAndCRC[payloadLen:])
		expectedPayloadCRC := crc32.ChecksumIEEE(payloadAndCRC[:payloadLen])
		if payloadCRC != expectedPayloadCRC {
			break // Corrupt payload
		}

		// Record is valid
		offset += int64(HeaderSize) + int64(payloadLen) + 4
		lastValidOffset = offset
	}

	return lastValidOffset, nil
}

// segmentPath returns the path for a segment ID
func (w *WALWriter) segmentPath(segmentID uint64) string {
	return filepath.Join(w.dir, fmt.Sprintf("wal_%012d.seg", segmentID))
}

// Append writes a record and returns the assigned LSN
// Thread-safe: uses mutex internally
func (w *WALWriter) Append(recType RecordType, payload []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("WAL writer is closed")
	}

	// Assign LSN atomically
	lsn := atomic.AddUint64(&w.lsn, 1) - 1

	// Create record
	rec, err := NewRecord(recType, lsn, payload)
	if err != nil {
		return 0, fmt.Errorf("failed to create record: %w", err)
	}

	// Encode record
	data := rec.Encode()

	// Write to file
	n, err := w.file.Write(data)
	if err != nil {
		return 0, fmt.Errorf("failed to write record: %w", err)
	}
	if n != len(data) {
		return 0, fmt.Errorf("short write: %d < %d", n, len(data))
	}

	w.offset += int64(n)
	w.pendingWrites++

	// Sync if immediate or batch size reached
	if w.syncPolicy.Immediate {
		if err := w.syncLocked(); err != nil {
			return 0, fmt.Errorf("failed to sync: %w", err)
		}
	} else if w.syncPolicy.BatchSize > 0 && w.pendingWrites >= w.syncPolicy.BatchSize {
		if err := w.syncLocked(); err != nil {
			return 0, fmt.Errorf("failed to sync: %w", err)
		}
	}

	// Check if we need to rotate
	if w.offset >= w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, fmt.Errorf("failed to rotate segment: %w", err)
		}
	}

	return lsn, nil
}

// AppendWithSync writes a record and syncs immediately, returning the LSN
func (w *WALWriter) AppendWithSync(recType RecordType, payload []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("WAL writer is closed")
	}

	// Assign LSN atomically
	lsn := atomic.AddUint64(&w.lsn, 1) - 1

	// Create and encode record
	rec, err := NewRecord(recType, lsn, payload)
	if err != nil {
		return 0, fmt.Errorf("failed to create record: %w", err)
	}

	data := rec.Encode()

	// Write and sync
	n, err := w.file.Write(data)
	if err != nil {
		return 0, fmt.Errorf("failed to write record: %w", err)
	}
	if n != len(data) {
		return 0, fmt.Errorf("short write: %d < %d", n, len(data))
	}

	if err := w.file.Sync(); err != nil {
		return 0, fmt.Errorf("failed to sync: %w", err)
	}

	w.offset += int64(n)
	w.pendingWrites = 0
	w.lastSync = time.Now()

	// Check rotation
	if w.offset >= w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, fmt.Errorf("failed to rotate segment: %w", err)
		}
	}

	return lsn, nil
}

// Sync forces fsync to disk
func (w *WALWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.syncLocked()
}

// syncLocked syncs while holding the mutex
func (w *WALWriter) syncLocked() error {
	if w.file == nil || w.pendingWrites == 0 {
		return nil
	}

	if err := w.file.Sync(); err != nil {
		return err
	}

	w.pendingWrites = 0
	w.lastSync = time.Now()
	return nil
}

// rotateLocked rotates to a new segment while holding the mutex
func (w *WALWriter) rotateLocked() error {
	// Sync current segment
	if err := w.syncLocked(); err != nil {
		return err
	}

	oldSegmentID := w.segmentID
	oldPath := w.segmentPath(oldSegmentID)

	// Close current file
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close segment: %w", err)
	}

	// Update manifest if available
	if w.manifest != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Calculate checksum of sealed segment
		checksum, err := CalculateSegmentChecksum(oldPath)
		if err != nil {
			return fmt.Errorf("failed to calculate segment checksum: %w", err)
		}

		if err := w.manifest.SealSegment(ctx, oldSegmentID, checksum); err != nil {
			return fmt.Errorf("failed to seal segment in manifest: %w", err)
		}
	}

	// Create new segment
	w.segmentID++

	if w.manifest != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		newPath := w.segmentPath(w.segmentID)
		if err := w.manifest.CreateSegment(ctx, w.segmentID, newPath); err != nil {
			return fmt.Errorf("failed to create segment in manifest: %w", err)
		}
	}

	// Open new file
	if err := w.openSegment(); err != nil {
		return err
	}

	return nil
}

// startBackgroundSync starts the background sync goroutine
func (w *WALWriter) startBackgroundSync() {
	w.syncTicker = time.NewTicker(w.syncPolicy.Interval)
	w.wg.Add(1)

	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-w.syncTicker.C:
				w.mu.Lock()
				if w.pendingWrites > 0 {
					_ = w.syncLocked()
				}
				w.mu.Unlock()
			case <-w.stopSync:
				return
			}
		}
	}()
}

// Close flushes and closes the current segment
func (w *WALWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true

	// Stop background sync
	if w.syncTicker != nil {
		w.syncTicker.Stop()
		close(w.stopSync)
	}

	// Wait for background goroutine
	w.mu.Unlock()
	w.wg.Wait()
	w.mu.Lock()

	// Sync and close file
	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync on close: %w", err)
		}
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("failed to close segment: %w", err)
		}
	}

	return nil
}

// CurrentLSN returns the next LSN to be assigned
func (w *WALWriter) CurrentLSN() uint64 {
	return atomic.LoadUint64(&w.lsn)
}

// CurrentSegmentID returns the current segment ID
func (w *WALWriter) CurrentSegmentID() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.segmentID
}

// CurrentOffset returns the current offset in the segment
func (w *WALWriter) CurrentOffset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.offset
}

// Dir returns the WAL directory
func (w *WALWriter) Dir() string {
	return w.dir
}
