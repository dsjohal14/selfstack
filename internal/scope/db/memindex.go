package db

import (
	"sort"
	"sync"

	"github.com/dsjohal14/selfstack/internal/relay"
	"github.com/dsjohal14/selfstack/internal/scope/db/wal"
)

// MemIndex is a thread-safe in-memory index of documents
type MemIndex struct {
	mu   sync.RWMutex
	docs map[string]Document
}

// NewMemIndex creates a new empty in-memory index
func NewMemIndex() *MemIndex {
	return &MemIndex{
		docs: make(map[string]Document),
	}
}

// Set adds or updates a document in the index
func (m *MemIndex) Set(docID string, doc Document) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[docID] = doc
}

// SetRecovered adds a document from WAL recovery
// Implements wal.DocumentIndex interface
func (m *MemIndex) SetRecovered(doc wal.RecoveredDoc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.DocID] = Document{
		ID:        doc.DocID,
		Source:    doc.Source,
		Title:     doc.Title,
		Text:      doc.Text,
		Metadata:  doc.Metadata,
		CreatedAt: doc.CreatedAt,
		Embedding: doc.Embedding,
	}
}

// Delete removes a document from the index
func (m *MemIndex) Delete(docID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, docID)
}

// Get retrieves a document by ID
func (m *MemIndex) Get(docID string) (Document, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	doc, ok := m.docs[docID]
	return doc, ok
}

// Count returns the number of documents in the index
func (m *MemIndex) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.docs)
}

// All returns all documents in the index (copy)
func (m *MemIndex) All() []Document {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Document, 0, len(m.docs))
	for _, doc := range m.docs {
		result = append(result, doc)
	}
	return result
}

// AllIDs returns all document IDs in the index
func (m *MemIndex) AllIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0, len(m.docs))
	for id := range m.docs {
		result = append(result, id)
	}
	return result
}

// Search finds documents similar to the query embedding
func (m *MemIndex) Search(query relay.Embedding, limit int) []SearchResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.docs) == 0 {
		return nil
	}

	results := make([]SearchResult, 0, len(m.docs))
	for _, doc := range m.docs {
		score := relay.CosineSimilarity(query, doc.Embedding)
		results = append(results, SearchResult{
			DocID:     doc.ID,
			Score:     score,
			Title:     doc.Title,
			Text:      doc.Text,
			Source:    doc.Source,
			Metadata:  doc.Metadata,
			CreatedAt: doc.CreatedAt,
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}

	return results
}

// Clear removes all documents from the index
func (m *MemIndex) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs = make(map[string]Document)
}

// Has checks if a document exists in the index
func (m *MemIndex) Has(docID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.docs[docID]
	return ok
}

// Range iterates over all documents in the index
// The callback should return false to stop iteration
func (m *MemIndex) Range(fn func(docID string, doc Document) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for id, doc := range m.docs {
		if !fn(id, doc) {
			break
		}
	}
}

// Clone creates a deep copy of the index
func (m *MemIndex) Clone() *MemIndex {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clone := NewMemIndex()
	for id, doc := range m.docs {
		clone.docs[id] = doc
	}
	return clone
}
