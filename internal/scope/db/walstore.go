package db

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
	"github.com/dsjohal14/selfstack/internal/scope/db/wal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WALStore is a WAL-backed document store with durable writes
type WALStore struct {
	dataDir    string
	walDir     string
	index      *MemIndex
	writer     *wal.WALWriter
	manifest   wal.ManifestStore
	db         *pgxpool.Pool
	compactor  *wal.Compactor
	mu         sync.RWMutex
	closed     bool
	syncPolicy wal.SyncPolicy // Track sync policy for Add operations
}

// WALStoreConfig holds configuration for WALStore
type WALStoreConfig struct {
	// DataDir is the base data directory
	DataDir string

	// WALDir is the WAL directory (defaults to DataDir/wal)
	WALDir string

	// DB is the optional Postgres connection pool
	DB *pgxpool.Pool

	// SyncPolicy controls when to fsync
	SyncPolicy wal.SyncPolicy

	// MaxSegmentSize is the max segment size before rotation
	MaxSegmentSize int64

	// EnableCompaction enables background compaction
	EnableCompaction bool

	// CompactionConfig is the compaction configuration
	CompactionConfig wal.CompactorConfig
}

// DefaultWALStoreConfig returns a default configuration
func DefaultWALStoreConfig(dataDir string) WALStoreConfig {
	return WALStoreConfig{
		DataDir:          dataDir,
		WALDir:           filepath.Join(dataDir, "wal"),
		SyncPolicy:       wal.ImmediateSyncPolicy(),
		MaxSegmentSize:   wal.DefaultMaxSegmentSize,
		EnableCompaction: false,
		CompactionConfig: wal.DefaultCompactorConfig(),
	}
}

// NewWALStore creates a new WAL-backed store
func NewWALStore(ctx context.Context, config WALStoreConfig) (*WALStore, error) {
	// Create index
	index := NewMemIndex()

	// Create WAL directory
	walDir := config.WALDir
	if walDir == "" {
		walDir = filepath.Join(config.DataDir, "wal")
	}

	// Setup manifest
	var manifest wal.ManifestStore
	if config.DB != nil {
		manifest = wal.NewPostgresManifest(config.DB)
	} else {
		manifest = wal.NewInMemoryManifest()
	}

	store := &WALStore{
		dataDir:    config.DataDir,
		walDir:     walDir,
		index:      index,
		manifest:   manifest,
		db:         config.DB,
		syncPolicy: config.SyncPolicy,
	}

	// Run recovery FIRST to determine correct LSN and segment ID
	// This handles both manifest-based and file-based recovery
	recoveryStats, err := store.recoverAndGetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	// Determine initial LSN and segment ID from recovery
	// Use the higher of: manifest state OR recovered max LSN + 1
	var initialLSN uint64 = 1
	var initialSegmentID uint64 = 1

	if config.DB != nil {
		state, err := manifest.GetWALState(ctx)
		if err == nil && state != nil {
			initialLSN = state.NextLSN
			initialSegmentID = state.CurrentSegmentID
		}
	}

	// CRITICAL: Use max of manifest state and recovered state to prevent LSN rewind
	if recoveryStats != nil && recoveryStats.MaxLSN >= initialLSN {
		initialLSN = recoveryStats.MaxLSN + 1
	}

	// Find latest WAL segment from file system to prevent segment ID collision.
	// Use WAL-only listing to avoid using compacted segment IDs which live in a
	// separate namespace and don't affect WAL writer ID allocation.
	_, latestSegID, err := wal.FindLatestWALSegment(walDir)
	if err == nil && latestSegID >= initialSegmentID {
		initialSegmentID = latestSegID
	}

	// Create WAL writer options with corrected LSN and segment ID
	opts := []wal.WALWriterOption{
		wal.WithSyncPolicy(config.SyncPolicy),
		wal.WithManifest(manifest),
		wal.WithInitialLSN(initialLSN),
		wal.WithInitialSegmentID(initialSegmentID),
	}
	if config.MaxSegmentSize > 0 {
		opts = append(opts, wal.WithMaxSegmentSize(config.MaxSegmentSize))
	}

	// Create WAL writer
	writer, err := wal.NewWALWriter(walDir, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL writer: %w", err)
	}
	store.writer = writer

	// Register initial segment in manifest
	if config.DB != nil {
		segPath := filepath.Join(walDir, fmt.Sprintf("wal_%012d.seg", initialSegmentID))
		// Ignore error if segment already exists
		_ = manifest.CreateSegment(ctx, initialSegmentID, segPath)

		// Update WAL state with correct LSN after recovery
		_ = manifest.UpdateWALState(ctx, initialSegmentID, initialLSN)
	}

	// Setup compactor if enabled
	if config.EnableCompaction && config.DB != nil {
		compactConfig := config.CompactionConfig
		if compactConfig.TmpDir == "" {
			compactConfig.TmpDir = filepath.Join(walDir, ".tmp")
		}
		store.compactor = wal.NewCompactor(manifest, config.DB, walDir, compactConfig)
	}

	// Start compactor if enabled - use background context so it survives init timeout
	if store.compactor != nil {
		if err := store.compactor.Start(context.Background()); err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("failed to start compactor: %w", err)
		}
	}

	fmt.Printf("WAL store initialized: %d documents, next LSN=%d, segment=%d\n",
		store.index.Count(), initialLSN, initialSegmentID)

	return store, nil
}

// recoverAndGetStats rebuilds the in-memory index from WAL and returns stats
// Uses single-pass file-based recovery to avoid stale manifest overwriting newer data
func (s *WALStore) recoverAndGetStats(ctx context.Context) (*wal.RecoveryStats, error) {
	rm := wal.NewRecoveryManager(s.manifest, s.walDir, s.index)

	// Single-pass file-based recovery - scans all WAL files in order
	// This is the authoritative source of truth for document state
	stats, err := rm.RecoverWithoutManifest(ctx)
	if err != nil {
		return nil, fmt.Errorf("file-based recovery failed: %w", err)
	}

	// If we have a Postgres manifest, get max LSN from manifest metadata
	// but do NOT re-apply records (would cause stale overwrites)
	if s.db != nil {
		state, stateErr := s.manifest.GetWALState(ctx)
		if stateErr == nil && state != nil {
			// Use higher of file-based max LSN or manifest next LSN
			if state.NextLSN > stats.MaxLSN+1 {
				stats.MaxLSN = state.NextLSN - 1
			}
		}
	}

	fmt.Printf("WAL recovery complete: loaded %d records from %d segments in %v\n",
		stats.RecordsLoaded, stats.SegmentsLoaded, stats.RecoveryTime)

	return stats, nil
}

// Add adds a document to the store with WAL durability
func (s *WALStore) Add(doc Document) error {
	return s.AddWithContext(context.Background(), doc)
}

// AddWithContext adds a document with context
func (s *WALStore) AddWithContext(_ context.Context, doc Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	// Determine record type (INSERT or UPDATE)
	recType := wal.RecordTypeInsert
	if s.index.Has(doc.ID) {
		recType = wal.RecordTypeUpdate
	}

	// Encode payload
	meta := wal.DocMetadata{
		Source:    doc.Source,
		Title:     doc.Title,
		Text:      doc.Text,
		Metadata:  doc.Metadata,
		CreatedAt: doc.CreatedAt,
	}
	payload, err := wal.EncodeDocPayload(doc.ID, meta, doc.Embedding)
	if err != nil {
		return fmt.Errorf("failed to encode payload: %w", err)
	}

	// Write to WAL - use sync policy from config
	if s.syncPolicy.Immediate {
		_, err = s.writer.AppendWithSync(recType, payload)
	} else {
		_, err = s.writer.Append(recType, payload)
	}
	if err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}

	// Update in-memory index
	s.index.Set(doc.ID, doc)

	return nil
}

// Delete marks a document for deletion (tombstone)
func (s *WALStore) Delete(docID string) error {
	return s.DeleteWithContext(context.Background(), docID)
}

// DeleteWithContext marks a document for deletion with context
func (s *WALStore) DeleteWithContext(_ context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("store is closed")
	}

	// Encode delete payload
	payload, err := wal.EncodeDeletePayload(docID)
	if err != nil {
		return fmt.Errorf("failed to encode delete payload: %w", err)
	}

	// Write tombstone to WAL - use sync policy from config
	if s.syncPolicy.Immediate {
		_, err = s.writer.AppendWithSync(wal.RecordTypeDelete, payload)
	} else {
		_, err = s.writer.Append(wal.RecordTypeDelete, payload)
	}
	if err != nil {
		return fmt.Errorf("failed to write tombstone to WAL: %w", err)
	}

	// Update in-memory index
	s.index.Delete(docID)

	return nil
}

// Get retrieves a document by ID
func (s *WALStore) Get(docID string) (Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.Get(docID)
}

// Search finds documents similar to the query embedding
func (s *WALStore) Search(query relay.Embedding, limit int) []SearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.Search(query, limit)
}

// Count returns the number of documents in the store
func (s *WALStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.Count()
}

// Flush syncs pending writes to disk
// For immediate sync policy, this is a no-op since each write already syncs
// For batched sync policy, this flushes any pending writes
func (s *WALStore) Flush() error {
	if s.syncPolicy.Immediate {
		return nil // Already synced on each write
	}
	return s.writer.Sync()
}

// Close flushes and closes the store
func (s *WALStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	// Stop compactor
	if s.compactor != nil {
		s.compactor.Stop()
	}

	// Close WAL writer
	if err := s.writer.Close(); err != nil {
		return fmt.Errorf("failed to close WAL writer: %w", err)
	}

	// Update final WAL state in manifest
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.manifest.UpdateWALState(ctx, s.writer.CurrentSegmentID(), s.writer.CurrentLSN())
	}

	return nil
}

// WriteCheckpoint writes a checkpoint record to the WAL
func (s *WALStore) WriteCheckpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := wal.EncodeCheckpointPayload(s.writer.CurrentLSN())
	if err != nil {
		return err
	}

	_, err = s.writer.AppendWithSync(wal.RecordTypeCheckpoint, payload)
	return err
}

// ForceCompaction triggers a compaction run
func (s *WALStore) ForceCompaction(ctx context.Context) error {
	if s.compactor == nil {
		return fmt.Errorf("compaction not enabled")
	}
	return s.compactor.ForceCompact(ctx)
}

// Index returns the underlying MemIndex for direct access
func (s *WALStore) Index() *MemIndex {
	return s.index
}
