// Package db provides database storage and document management for Selfstack.
package db

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
)

// Document represents a stored document matching the Doc contract schema
type Document struct {
	ID        string            `json:"id"`     // UUID format
	Source    string            `json:"source"` // Source identifier
	Title     string            `json:"title"`  // Document title
	Text      string            `json:"text"`   // Full content
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Embedding relay.Embedding   `json:"-"` // Not stored in JSONL, stored in binary
}

// Store manages on-disk storage of documents and their embeddings
type Store struct {
	dataDir  string
	mu       sync.RWMutex
	docs     []Document // In-memory cache
	modified bool
}

// NewStore creates a new store with the given data directory
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	s := &Store{
		dataDir: dataDir,
		docs:    make([]Document, 0),
	}

	// Load existing data if present
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load store: %w", err)
	}

	return s, nil
}

// Add adds a document to the store
func (s *Store) Add(doc Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if doc with same ID exists, update if so
	for i := range s.docs {
		if s.docs[i].ID == doc.ID {
			s.docs[i] = doc
			s.modified = true
			return nil
		}
	}

	s.docs = append(s.docs, doc)
	s.modified = true
	return nil
}

// Search finds documents similar to the query embedding
func (s *Store) Search(query relay.Embedding, limit int) []SearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]SearchResult, 0, len(s.docs))

	for i := range s.docs {
		score := relay.CosineSimilarity(query, s.docs[i].Embedding)
		results = append(results, SearchResult{
			DocID:     s.docs[i].ID,
			Score:     score,
			Title:     s.docs[i].Title,
			Text:      s.docs[i].Text,
			Source:    s.docs[i].Source,
			Metadata:  s.docs[i].Metadata,
			CreatedAt: s.docs[i].CreatedAt,
		})
	}

	// Sort by score descending (simple bubble sort for small datasets)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}

	return results
}

// SearchResult represents a search result with score
type SearchResult struct {
	DocID     string            `json:"doc_id"`
	Score     float32           `json:"score"`
	Title     string            `json:"title"`
	Text      string            `json:"text"`
	Source    string            `json:"source"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Count returns the number of documents in the store
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

// Flush writes the store to disk
func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.modified {
		return nil // No changes to write
	}

	if err := s.writeMetadata(); err != nil {
		return err
	}

	if err := s.writeVectors(); err != nil {
		return err
	}

	s.modified = false
	return nil
}

// Close flushes and closes the store
func (s *Store) Close() error {
	return s.Flush()
}

// writeMetadata writes document metadata to JSONL file
func (s *Store) writeMetadata() error {
	path := filepath.Join(s.dataDir, "metadata.jsonl")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer func() { _ = f.Close() }()

	encoder := json.NewEncoder(f)
	for i := range s.docs {
		if err := encoder.Encode(s.docs[i]); err != nil {
			return fmt.Errorf("failed to encode document %d: %w", i, err)
		}
	}

	return nil
}

// writeVectors writes embeddings to binary file
func (s *Store) writeVectors() error {
	path := filepath.Join(s.dataDir, "vectors.bin")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create vectors file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Write header: [num_docs:uint32][dim:uint32]
	if err := binary.Write(f, binary.LittleEndian, uint32(len(s.docs))); err != nil {
		return err
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(relay.EmbeddingDim)); err != nil {
		return err
	}

	// Write vectors as float32 arrays
	for i := range s.docs {
		if err := binary.Write(f, binary.LittleEndian, s.docs[i].Embedding); err != nil {
			return fmt.Errorf("failed to write vector %d: %w", i, err)
		}
	}

	return nil
}

// load reads store from disk
func (s *Store) load() error {
	if err := s.loadMetadata(); err != nil {
		return err
	}
	if err := s.loadVectors(); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadMetadata() error {
	path := filepath.Join(s.dataDir, "metadata.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	s.docs = make([]Document, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var doc Document
		if err := json.Unmarshal(scanner.Bytes(), &doc); err != nil {
			return fmt.Errorf("failed to decode document: %w", err)
		}
		s.docs = append(s.docs, doc)
	}

	return scanner.Err()
}

func (s *Store) loadVectors() error {
	path := filepath.Join(s.dataDir, "vectors.bin")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Read header
	var numDocs, dim uint32
	if err := binary.Read(f, binary.LittleEndian, &numDocs); err != nil {
		return err
	}
	if err := binary.Read(f, binary.LittleEndian, &dim); err != nil {
		return err
	}

	if int(numDocs) != len(s.docs) {
		return fmt.Errorf("vector count mismatch: expected %d, got %d", len(s.docs), numDocs)
	}
	if dim != relay.EmbeddingDim {
		return fmt.Errorf("dimension mismatch: expected %d, got %d", relay.EmbeddingDim, dim)
	}

	// Read vectors
	for i := 0; i < int(numDocs); i++ {
		if err := binary.Read(f, binary.LittleEndian, &s.docs[i].Embedding); err != nil {
			if err == io.EOF {
				return fmt.Errorf("unexpected EOF at vector %d", i)
			}
			return fmt.Errorf("failed to read vector %d: %w", i, err)
		}
	}

	return nil
}
