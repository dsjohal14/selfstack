package wal

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SegmentStatus represents the lifecycle status of a segment
type SegmentStatus string

// Segment status values
const (
	SegmentStatusActive     SegmentStatus = "active"
	SegmentStatusSealed     SegmentStatus = "sealed"
	SegmentStatusCompacting SegmentStatus = "compacting"
	SegmentStatusArchived   SegmentStatus = "archived"
)

// SegmentType distinguishes between WAL segments and compacted segments
type SegmentType string

// Segment type values
const (
	SegmentTypeWAL       SegmentType = "wal"
	SegmentTypeCompacted SegmentType = "cmp"
)

// SegmentInfo contains metadata about a WAL segment
type SegmentInfo struct {
	ID          int64
	SegmentID   uint64
	SegmentType SegmentType
	Filename    string
	SizeBytes   int64
	RecordCount int
	MinLSN      *uint64
	MaxLSN      *uint64
	Status      SegmentStatus
	CreatedAt   time.Time
	SealedAt    *time.Time
	Checksum    *string
}

// WALState contains the global WAL state
//
//nolint:revive // WALState name is intentional for clarity
type WALState struct {
	CurrentSegmentID uint64
	NextLSN          uint64
	CheckpointLSN    uint64
	UpdatedAt        time.Time
}

// RecoveryInfo contains information needed for WAL recovery
type RecoveryInfo struct {
	State    WALState
	Segments []SegmentInfo
}

// ManifestStore defines the interface for WAL manifest storage
type ManifestStore interface {
	// GetActiveSegment returns the current active WAL segment
	GetActiveSegment(ctx context.Context) (*SegmentInfo, error)

	// CreateSegment registers a new WAL segment (segment_type='wal')
	CreateSegment(ctx context.Context, segmentID uint64, filename string) error

	// CreateCompactedSegment registers a new compacted segment (segment_type='cmp')
	CreateCompactedSegment(ctx context.Context, segmentID uint64, filename string, sizeBytes int64, recordCount int, minLSN, maxLSN uint64, checksum string) error

	// SealSegment marks a WAL segment as sealed with its checksum
	SealSegment(ctx context.Context, segmentID uint64, checksum string) error

	// UpdateSegmentStats updates segment statistics
	UpdateSegmentStats(ctx context.Context, segmentID uint64, sizeBytes int64, recordCount int, minLSN, maxLSN uint64) error

	// GetSealedSegments returns all sealed segments (both WAL and compacted)
	GetSealedSegments(ctx context.Context) ([]SegmentInfo, error)

	// GetSealedWALSegments returns only sealed WAL segments (for compaction input)
	GetSealedWALSegments(ctx context.Context) ([]SegmentInfo, error)

	// GetSegmentsByStatus returns all segments (WAL and compacted) with the given status.
	// Use GetSealedWALSegments if you need WAL-only segments.
	GetSegmentsByStatus(ctx context.Context, status SegmentStatus) ([]SegmentInfo, error)

	// UpdateSegmentStatus updates a WAL segment's status.
	// This is an alias for UpdateWALSegmentStatus for backward compatibility.
	// For compacted segments, use direct SQL in a transaction.
	UpdateSegmentStatus(ctx context.Context, segmentID uint64, status SegmentStatus) error

	// UpdateWALSegmentStatus updates a WAL segment's status
	UpdateWALSegmentStatus(ctx context.Context, segmentID uint64, status SegmentStatus) error

	// ArchiveSegments marks multiple WAL segments as archived.
	// Only operates on WAL segments (segment_type='wal').
	ArchiveSegments(ctx context.Context, segmentIDs []uint64) error

	// GetWALState returns the current WAL state
	GetWALState(ctx context.Context) (*WALState, error)

	// UpdateWALState updates the WAL state
	UpdateWALState(ctx context.Context, currentSegmentID, nextLSN uint64) error

	// UpdateCheckpointLSN updates the checkpoint LSN
	UpdateCheckpointLSN(ctx context.Context, lsn uint64) error

	// GetRecoveryInfo returns all information needed for recovery
	GetRecoveryInfo(ctx context.Context) (*RecoveryInfo, error)
}

// PostgresManifest implements ManifestStore using PostgreSQL
type PostgresManifest struct {
	db *pgxpool.Pool
}

// NewPostgresManifest creates a new PostgreSQL-backed manifest store
func NewPostgresManifest(db *pgxpool.Pool) *PostgresManifest {
	return &PostgresManifest{db: db}
}

// GetActiveSegment returns the current active WAL segment
func (m *PostgresManifest) GetActiveSegment(ctx context.Context) (*SegmentInfo, error) {
	var seg SegmentInfo
	var minLSN, maxLSN *int64
	var sealedAt *time.Time
	var checksum *string

	err := m.db.QueryRow(ctx, `
		SELECT id, segment_id, segment_type, filename, size_bytes, record_count,
		       min_lsn, max_lsn, status, created_at, sealed_at, checksum
		FROM wal_segments
		WHERE status = 'active' AND segment_type = 'wal'
		ORDER BY segment_id DESC
		LIMIT 1
	`).Scan(
		&seg.ID, &seg.SegmentID, &seg.SegmentType, &seg.Filename, &seg.SizeBytes, &seg.RecordCount,
		&minLSN, &maxLSN, &seg.Status, &seg.CreatedAt, &sealedAt, &checksum,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active segment: %w", err)
	}

	if minLSN != nil {
		v := uint64(*minLSN)
		seg.MinLSN = &v
	}
	if maxLSN != nil {
		v := uint64(*maxLSN)
		seg.MaxLSN = &v
	}
	seg.SealedAt = sealedAt
	seg.Checksum = checksum

	return &seg, nil
}

// CreateSegment registers a new WAL segment (segment_type='wal')
func (m *PostgresManifest) CreateSegment(ctx context.Context, segmentID uint64, filename string) error {
	_, err := m.db.Exec(ctx, `
		INSERT INTO wal_segments (segment_id, segment_type, filename, status, created_at)
		VALUES ($1, 'wal', $2, 'active', NOW())
	`, segmentID, filename)
	if err != nil {
		return fmt.Errorf("failed to create segment: %w", err)
	}
	return nil
}

// CreateCompactedSegment registers a new compacted segment (segment_type='cmp')
func (m *PostgresManifest) CreateCompactedSegment(ctx context.Context, segmentID uint64, filename string, sizeBytes int64, recordCount int, minLSN, maxLSN uint64, checksum string) error {
	_, err := m.db.Exec(ctx, `
		INSERT INTO wal_segments (segment_id, segment_type, filename, size_bytes, record_count, min_lsn, max_lsn, status, checksum, sealed_at, created_at)
		VALUES ($1, 'cmp', $2, $3, $4, $5, $6, 'sealed', $7, NOW(), NOW())
	`, segmentID, filename, sizeBytes, recordCount, minLSN, maxLSN, checksum)
	if err != nil {
		return fmt.Errorf("failed to create compacted segment: %w", err)
	}
	return nil
}

// SealSegment marks a WAL segment as sealed with its checksum
func (m *PostgresManifest) SealSegment(ctx context.Context, segmentID uint64, checksum string) error {
	result, err := m.db.Exec(ctx, `
		UPDATE wal_segments
		SET status = 'sealed', sealed_at = NOW(), checksum = $2
		WHERE segment_id = $1 AND segment_type = 'wal' AND status = 'active'
	`, segmentID, checksum)
	if err != nil {
		return fmt.Errorf("failed to seal segment: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("segment %d not found or not active", segmentID)
	}
	return nil
}

// UpdateSegmentStats updates WAL segment statistics
func (m *PostgresManifest) UpdateSegmentStats(ctx context.Context, segmentID uint64, sizeBytes int64, recordCount int, minLSN, maxLSN uint64) error {
	_, err := m.db.Exec(ctx, `
		UPDATE wal_segments
		SET size_bytes = $2, record_count = $3, min_lsn = $4, max_lsn = $5
		WHERE segment_id = $1 AND segment_type = 'wal'
	`, segmentID, sizeBytes, recordCount, minLSN, maxLSN)
	if err != nil {
		return fmt.Errorf("failed to update segment stats: %w", err)
	}
	return nil
}

// GetSealedSegments returns all sealed segments (both WAL and compacted) ordered by segment_id
func (m *PostgresManifest) GetSealedSegments(ctx context.Context) ([]SegmentInfo, error) {
	return m.GetSegmentsByStatus(ctx, SegmentStatusSealed)
}

// GetSealedWALSegments returns only sealed WAL segments (for compaction input)
func (m *PostgresManifest) GetSealedWALSegments(ctx context.Context) ([]SegmentInfo, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, segment_id, segment_type, filename, size_bytes, record_count,
		       min_lsn, max_lsn, status, created_at, sealed_at, checksum
		FROM wal_segments
		WHERE status = 'sealed' AND segment_type = 'wal'
		ORDER BY segment_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get sealed WAL segments: %w", err)
	}
	defer rows.Close()

	return m.scanSegments(rows)
}

// GetSegmentsByStatus returns segments with the given status
func (m *PostgresManifest) GetSegmentsByStatus(ctx context.Context, status SegmentStatus) ([]SegmentInfo, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, segment_id, segment_type, filename, size_bytes, record_count,
		       min_lsn, max_lsn, status, created_at, sealed_at, checksum
		FROM wal_segments
		WHERE status = $1
		ORDER BY segment_id ASC
	`, status)
	if err != nil {
		return nil, fmt.Errorf("failed to get segments by status: %w", err)
	}
	defer rows.Close()

	return m.scanSegments(rows)
}

// scanSegments is a helper to scan segment rows into SegmentInfo slice
func (m *PostgresManifest) scanSegments(rows pgx.Rows) ([]SegmentInfo, error) {
	var segments []SegmentInfo
	for rows.Next() {
		var seg SegmentInfo
		var minLSN, maxLSN *int64
		var sealedAt *time.Time
		var checksum *string

		err := rows.Scan(
			&seg.ID, &seg.SegmentID, &seg.SegmentType, &seg.Filename, &seg.SizeBytes, &seg.RecordCount,
			&minLSN, &maxLSN, &seg.Status, &seg.CreatedAt, &sealedAt, &checksum,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan segment: %w", err)
		}

		if minLSN != nil {
			v := uint64(*minLSN)
			seg.MinLSN = &v
		}
		if maxLSN != nil {
			v := uint64(*maxLSN)
			seg.MaxLSN = &v
		}
		seg.SealedAt = sealedAt
		seg.Checksum = checksum

		segments = append(segments, seg)
	}

	return segments, rows.Err()
}

// UpdateSegmentStatus updates a segment's status (defaults to WAL type for backward compat)
func (m *PostgresManifest) UpdateSegmentStatus(ctx context.Context, segmentID uint64, status SegmentStatus) error {
	return m.UpdateWALSegmentStatus(ctx, segmentID, status)
}

// UpdateWALSegmentStatus updates a WAL segment's status
func (m *PostgresManifest) UpdateWALSegmentStatus(ctx context.Context, segmentID uint64, status SegmentStatus) error {
	result, err := m.db.Exec(ctx, `
		UPDATE wal_segments SET status = $2 WHERE segment_id = $1 AND segment_type = 'wal'
	`, segmentID, status)
	if err != nil {
		return fmt.Errorf("failed to update WAL segment status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("WAL segment %d not found", segmentID)
	}
	return nil
}

// ArchiveSegments marks multiple WAL segments as archived
func (m *PostgresManifest) ArchiveSegments(ctx context.Context, segmentIDs []uint64) error {
	if len(segmentIDs) == 0 {
		return nil
	}

	// Convert uint64 to int64 for pgx compatibility with bigint[]
	ids := make([]int64, len(segmentIDs))
	for i, id := range segmentIDs {
		ids[i] = int64(id)
	}

	_, err := m.db.Exec(ctx, `
		UPDATE wal_segments SET status = 'archived' WHERE segment_id = ANY($1) AND segment_type = 'wal'
	`, ids)
	if err != nil {
		return fmt.Errorf("failed to archive segments: %w", err)
	}
	return nil
}

// GetWALState returns the current WAL state
func (m *PostgresManifest) GetWALState(ctx context.Context) (*WALState, error) {
	var state WALState
	err := m.db.QueryRow(ctx, `
		SELECT current_segment_id, next_lsn, checkpoint_lsn, updated_at
		FROM wal_state
		WHERE id = 1
	`).Scan(&state.CurrentSegmentID, &state.NextLSN, &state.CheckpointLSN, &state.UpdatedAt)
	if err == pgx.ErrNoRows {
		// Return default state if not initialized
		return &WALState{
			CurrentSegmentID: 1,
			NextLSN:          1,
			CheckpointLSN:    0,
			UpdatedAt:        time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get WAL state: %w", err)
	}
	return &state, nil
}

// UpdateWALState updates the WAL state
func (m *PostgresManifest) UpdateWALState(ctx context.Context, currentSegmentID, nextLSN uint64) error {
	_, err := m.db.Exec(ctx, `
		INSERT INTO wal_state (id, current_segment_id, next_lsn, updated_at)
		VALUES (1, $1, $2, NOW())
		ON CONFLICT (id) DO UPDATE
		SET current_segment_id = $1, next_lsn = $2, updated_at = NOW()
	`, currentSegmentID, nextLSN)
	if err != nil {
		return fmt.Errorf("failed to update WAL state: %w", err)
	}
	return nil
}

// UpdateCheckpointLSN updates the checkpoint LSN
func (m *PostgresManifest) UpdateCheckpointLSN(ctx context.Context, lsn uint64) error {
	_, err := m.db.Exec(ctx, `
		UPDATE wal_state SET checkpoint_lsn = $1, updated_at = NOW() WHERE id = 1
	`, lsn)
	if err != nil {
		return fmt.Errorf("failed to update checkpoint LSN: %w", err)
	}
	return nil
}

// GetRecoveryInfo returns all information needed for recovery
func (m *PostgresManifest) GetRecoveryInfo(ctx context.Context) (*RecoveryInfo, error) {
	state, err := m.GetWALState(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get WAL state: %w", err)
	}

	// Get all non-archived segments (both WAL and compacted)
	rows, err := m.db.Query(ctx, `
		SELECT id, segment_id, segment_type, filename, size_bytes, record_count,
		       min_lsn, max_lsn, status, created_at, sealed_at, checksum
		FROM wal_segments
		WHERE status != 'archived'
		ORDER BY segment_type ASC, segment_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get segments for recovery: %w", err)
	}
	defer rows.Close()

	segments, err := m.scanSegments(rows)
	if err != nil {
		return nil, fmt.Errorf("error reading segments: %w", err)
	}

	return &RecoveryInfo{
		State:    *state,
		Segments: segments,
	}, nil
}

// segmentKey is a composite key for segment lookup by type and ID
type segmentKey struct {
	Type SegmentType
	ID   uint64
}

// InMemoryManifest implements ManifestStore using in-memory storage (for testing)
type InMemoryManifest struct {
	segments map[segmentKey]*SegmentInfo
	state    WALState
}

// NewInMemoryManifest creates a new in-memory manifest store
func NewInMemoryManifest() *InMemoryManifest {
	return &InMemoryManifest{
		segments: make(map[segmentKey]*SegmentInfo),
		state: WALState{
			CurrentSegmentID: 1,
			NextLSN:          1,
			CheckpointLSN:    0,
			UpdatedAt:        time.Now(),
		},
	}
}

// GetActiveSegment returns the current active WAL segment
func (m *InMemoryManifest) GetActiveSegment(_ context.Context) (*SegmentInfo, error) {
	for key, seg := range m.segments {
		if key.Type == SegmentTypeWAL && seg.Status == SegmentStatusActive {
			return seg, nil
		}
	}
	return nil, nil
}

// CreateSegment registers a new WAL segment (segment_type='wal')
func (m *InMemoryManifest) CreateSegment(_ context.Context, segmentID uint64, filename string) error {
	key := segmentKey{Type: SegmentTypeWAL, ID: segmentID}
	m.segments[key] = &SegmentInfo{
		ID:          int64(segmentID),
		SegmentID:   segmentID,
		SegmentType: SegmentTypeWAL,
		Filename:    filename,
		Status:      SegmentStatusActive,
		CreatedAt:   time.Now(),
	}
	m.state.CurrentSegmentID = segmentID
	return nil
}

// CreateCompactedSegment registers a new compacted segment (segment_type='cmp')
func (m *InMemoryManifest) CreateCompactedSegment(_ context.Context, segmentID uint64, filename string, sizeBytes int64, recordCount int, minLSN, maxLSN uint64, checksum string) error {
	key := segmentKey{Type: SegmentTypeCompacted, ID: segmentID}
	now := time.Now()
	m.segments[key] = &SegmentInfo{
		ID:          int64(segmentID),
		SegmentID:   segmentID,
		SegmentType: SegmentTypeCompacted,
		Filename:    filename,
		SizeBytes:   sizeBytes,
		RecordCount: recordCount,
		MinLSN:      &minLSN,
		MaxLSN:      &maxLSN,
		Status:      SegmentStatusSealed,
		CreatedAt:   now,
		SealedAt:    &now,
		Checksum:    &checksum,
	}
	return nil
}

// SealSegment marks a WAL segment as sealed with its checksum
func (m *InMemoryManifest) SealSegment(_ context.Context, segmentID uint64, checksum string) error {
	key := segmentKey{Type: SegmentTypeWAL, ID: segmentID}
	seg, ok := m.segments[key]
	if !ok {
		return fmt.Errorf("WAL segment %d not found", segmentID)
	}
	seg.Status = SegmentStatusSealed
	now := time.Now()
	seg.SealedAt = &now
	seg.Checksum = &checksum
	return nil
}

// UpdateSegmentStats updates WAL segment statistics
func (m *InMemoryManifest) UpdateSegmentStats(_ context.Context, segmentID uint64, sizeBytes int64, recordCount int, minLSN, maxLSN uint64) error {
	key := segmentKey{Type: SegmentTypeWAL, ID: segmentID}
	seg, ok := m.segments[key]
	if !ok {
		return fmt.Errorf("WAL segment %d not found", segmentID)
	}
	seg.SizeBytes = sizeBytes
	seg.RecordCount = recordCount
	seg.MinLSN = &minLSN
	seg.MaxLSN = &maxLSN
	return nil
}

// GetSealedSegments returns all sealed segments (both WAL and compacted)
func (m *InMemoryManifest) GetSealedSegments(ctx context.Context) ([]SegmentInfo, error) {
	return m.GetSegmentsByStatus(ctx, SegmentStatusSealed)
}

// GetSealedWALSegments returns only sealed WAL segments (for compaction input)
func (m *InMemoryManifest) GetSealedWALSegments(_ context.Context) ([]SegmentInfo, error) {
	var result []SegmentInfo
	for key, seg := range m.segments {
		if key.Type == SegmentTypeWAL && seg.Status == SegmentStatusSealed {
			result = append(result, *seg)
		}
	}
	return result, nil
}

// GetSegmentsByStatus returns segments with the given status (both WAL and compacted)
func (m *InMemoryManifest) GetSegmentsByStatus(_ context.Context, status SegmentStatus) ([]SegmentInfo, error) {
	var result []SegmentInfo
	for _, seg := range m.segments {
		if seg.Status == status {
			result = append(result, *seg)
		}
	}
	return result, nil
}

// UpdateSegmentStatus updates a segment's status (defaults to WAL type)
func (m *InMemoryManifest) UpdateSegmentStatus(ctx context.Context, segmentID uint64, status SegmentStatus) error {
	return m.UpdateWALSegmentStatus(ctx, segmentID, status)
}

// UpdateWALSegmentStatus updates a WAL segment's status
func (m *InMemoryManifest) UpdateWALSegmentStatus(_ context.Context, segmentID uint64, status SegmentStatus) error {
	key := segmentKey{Type: SegmentTypeWAL, ID: segmentID}
	seg, ok := m.segments[key]
	if !ok {
		return fmt.Errorf("WAL segment %d not found", segmentID)
	}
	seg.Status = status
	return nil
}

// ArchiveSegments marks multiple WAL segments as archived
func (m *InMemoryManifest) ArchiveSegments(_ context.Context, segmentIDs []uint64) error {
	for _, id := range segmentIDs {
		key := segmentKey{Type: SegmentTypeWAL, ID: id}
		if seg, ok := m.segments[key]; ok {
			seg.Status = SegmentStatusArchived
		}
	}
	return nil
}

// GetWALState returns the current WAL state
func (m *InMemoryManifest) GetWALState(_ context.Context) (*WALState, error) {
	return &m.state, nil
}

// UpdateWALState updates the WAL state
func (m *InMemoryManifest) UpdateWALState(_ context.Context, currentSegmentID, nextLSN uint64) error {
	m.state.CurrentSegmentID = currentSegmentID
	m.state.NextLSN = nextLSN
	m.state.UpdatedAt = time.Now()
	return nil
}

// UpdateCheckpointLSN updates the checkpoint LSN
func (m *InMemoryManifest) UpdateCheckpointLSN(_ context.Context, lsn uint64) error {
	m.state.CheckpointLSN = lsn
	m.state.UpdatedAt = time.Now()
	return nil
}

// GetRecoveryInfo returns all information needed for recovery
func (m *InMemoryManifest) GetRecoveryInfo(_ context.Context) (*RecoveryInfo, error) {
	var segments []SegmentInfo
	for _, seg := range m.segments {
		if seg.Status != SegmentStatusArchived {
			segments = append(segments, *seg)
		}
	}
	return &RecoveryInfo{
		State:    m.state,
		Segments: segments,
	}, nil
}
