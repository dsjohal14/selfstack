// Package search provides full-text search capabilities for Selfstack.
package search

// Engine represents a search backend
type Engine interface {
	Index(docID string, content string) error
	Search(query string, limit int) ([]string, error)
}

// MemoryEngine is an in-memory search implementation for testing
type MemoryEngine struct {
	docs map[string]string
}

// NewMemoryEngine creates a new in-memory search engine
func NewMemoryEngine() *MemoryEngine {
	return &MemoryEngine{
		docs: make(map[string]string),
	}
}

// Index adds a document to the index
func (e *MemoryEngine) Index(docID string, content string) error {
	e.docs[docID] = content
	return nil
}

// Search performs a simple substring search
func (e *MemoryEngine) Search(query string, limit int) ([]string, error) {
	var results []string

	for docID, content := range e.docs {
		if contains(content, query) {
			results = append(results, docID)
			if len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
