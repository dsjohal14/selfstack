package db

import (
	"github.com/dsjohal14/selfstack/internal/relay"
)

// Storage is the interface for document storage
// Both Store (file-based) and WALStore (WAL-backed) implement this interface
type Storage interface {
	// Add adds or updates a document
	Add(doc Document) error

	// Search finds documents similar to the query embedding
	Search(query relay.Embedding, limit int) []SearchResult

	// Count returns the number of documents
	Count() int

	// Flush persists any pending changes
	Flush() error

	// Close flushes and closes the storage
	Close() error
}

// Ensure both Store and WALStore implement Storage
var _ Storage = (*Store)(nil)
var _ Storage = (*WALStore)(nil)
