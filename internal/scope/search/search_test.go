package search

import (
	"testing"
)

func TestNewMemoryEngine(t *testing.T) {
	engine := NewMemoryEngine()
	if engine == nil {
		t.Fatal("NewMemoryEngine() returned nil")
	}
}

func TestIndex(t *testing.T) {
	engine := NewMemoryEngine()
	
	err := engine.Index("doc1", "hello world")
	if err != nil {
		t.Errorf("Index() failed: %v", err)
	}
	
	if len(engine.docs) != 1 {
		t.Errorf("expected 1 doc, got %d", len(engine.docs))
	}
}

func TestSearch(t *testing.T) {
	engine := NewMemoryEngine()
	
	engine.Index("doc1", "hello world")
	engine.Index("doc2", "goodbye world")
	engine.Index("doc3", "hello there")
	
	tests := []struct {
		name     string
		query    string
		limit    int
		expected int
	}{
		{"find hello", "hello", 10, 2},
		{"find world", "world", 10, 2},
		{"find goodbye", "goodbye", 10, 1},
		{"not found", "xyz", 10, 0},
		{"limit results", "world", 1, 1},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := engine.Search(tt.query, tt.limit)
			if err != nil {
				t.Errorf("Search() failed: %v", err)
			}
			
			if len(results) != tt.expected {
				t.Errorf("expected %d results, got %d", tt.expected, len(results))
			}
		})
	}
}

