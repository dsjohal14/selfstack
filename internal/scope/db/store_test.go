package db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
)

func TestNewStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	if store.Count() != 0 {
		t.Errorf("new store should be empty, got %d docs", store.Count())
	}
}

func TestAddAndSearch(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Add documents with full contract fields
	doc1 := Document{
		ID:        "doc1",
		Source:    "test",
		Title:     "Hello World",
		Text:      "hello world content",
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("hello world content"),
	}
	doc2 := Document{
		ID:        "doc2",
		Source:    "test",
		Title:     "Goodbye World",
		Text:      "goodbye world content",
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("goodbye world content"),
	}

	if err := store.Add(doc1); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := store.Add(doc2); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("expected 2 docs, got %d", store.Count())
	}

	// Search
	query := relay.DeterministicEmbed("hello world content")
	results := store.Search(query, 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result should be doc1 (exact match)
	if results[0].DocID != "doc1" {
		t.Errorf("expected doc1 as top result, got %s", results[0].DocID)
	}

	// Verify all contract fields are present
	if results[0].Title == "" {
		t.Error("expected title to be present")
	}
	if results[0].Source == "" {
		t.Error("expected source to be present")
	}
	if results[0].CreatedAt.IsZero() {
		t.Error("expected created_at to be present")
	}

	// Score should be very high (close to 1.0)
	if results[0].Score < 0.99 {
		t.Errorf("expected high score for exact match, got %f", results[0].Score)
	}
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store and add document
	{
		store, err := NewStore(tmpDir)
		if err != nil {
			t.Fatalf("NewStore failed: %v", err)
		}

		doc := Document{
			ID:        "persisted-doc",
			Source:    "test",
			Title:     "Persistent Data",
			Text:      "persistent data content",
			CreatedAt: time.Now(),
			Embedding: relay.DeterministicEmbed("persistent data content"),
			Metadata: map[string]string{
				"type": "test",
			},
		}

		if err := store.Add(doc); err != nil {
			t.Fatalf("Add failed: %v", err)
		}

		if err := store.Flush(); err != nil {
			t.Fatalf("Flush failed: %v", err)
		}

		_ = store.Close()
	}

	// Verify files exist
	metaPath := filepath.Join(tmpDir, "metadata.jsonl")
	vecPath := filepath.Join(tmpDir, "vectors.bin")

	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("metadata.jsonl not created")
	}
	if _, err := os.Stat(vecPath); os.IsNotExist(err) {
		t.Error("vectors.bin not created")
	}

	// Reload store
	{
		store, err := NewStore(tmpDir)
		if err != nil {
			t.Fatalf("NewStore (reload) failed: %v", err)
		}
		defer func() { _ = store.Close() }()

		if store.Count() != 1 {
			t.Errorf("reloaded store should have 1 doc, got %d", store.Count())
		}

		// Search should still work
		query := relay.DeterministicEmbed("persistent data content")
		results := store.Search(query, 1)

		if len(results) == 0 {
			t.Fatal("no results from reloaded store")
		}

		if results[0].DocID != "persisted-doc" {
			t.Errorf("expected persisted-doc, got %s", results[0].DocID)
		}

		// Verify all fields persisted
		if results[0].Title != "Persistent Data" {
			t.Errorf("expected title 'Persistent Data', got %s", results[0].Title)
		}
		if results[0].Source != "test" {
			t.Errorf("expected source 'test', got %s", results[0].Source)
		}
		if results[0].Metadata == nil || results[0].Metadata["type"] != "test" {
			t.Error("metadata not persisted correctly")
		}
	}
}

func TestSearchLimit(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Add 5 documents
	for i := 1; i <= 5; i++ {
		doc := Document{
			ID:        string(rune('a' + i - 1)),
			Source:    "test",
			Title:     "Document",
			Text:      "document content",
			CreatedAt: time.Now(),
			Embedding: relay.DeterministicEmbed("document content"),
		}
		_ = store.Add(doc)
	}

	query := relay.DeterministicEmbed("document content")
	results := store.Search(query, 3)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestUpdateExistingDocument(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Add initial document
	doc := Document{
		ID:        "doc1",
		Source:    "test",
		Title:     "Original Title",
		Text:      "original content",
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("original content"),
	}
	_ = store.Add(doc)

	if store.Count() != 1 {
		t.Fatalf("expected 1 doc, got %d", store.Count())
	}

	// Update with same ID
	updatedDoc := Document{
		ID:        "doc1",
		Source:    "test",
		Title:     "Updated Title",
		Text:      "updated content",
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("updated content"),
	}
	_ = store.Add(updatedDoc)

	// Should still be 1 document
	if store.Count() != 1 {
		t.Errorf("expected 1 doc after update, got %d", store.Count())
	}

	// Verify it's the updated version
	query := relay.DeterministicEmbed("updated content")
	results := store.Search(query, 1)

	if len(results) == 0 {
		t.Fatal("no results found")
	}

	if results[0].Title != "Updated Title" {
		t.Errorf("expected 'Updated Title', got %s", results[0].Title)
	}
}

func TestLoadMetadataWithoutVectors(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store, add doc, flush, then delete vectors.bin
	{
		store, err := NewStore(tmpDir)
		if err != nil {
			t.Fatalf("NewStore failed: %v", err)
		}

		doc := Document{
			ID:        "doc1",
			Source:    "test",
			Title:     "Test Document",
			Text:      "test content for embedding",
			CreatedAt: time.Now(),
			Embedding: relay.DeterministicEmbed("test content for embedding"),
		}
		_ = store.Add(doc)
		_ = store.Flush()
		_ = store.Close()
	}

	// Delete vectors.bin to simulate corrupted state
	vecPath := filepath.Join(tmpDir, "vectors.bin")
	if err := os.Remove(vecPath); err != nil {
		t.Fatalf("failed to remove vectors.bin: %v", err)
	}

	// Reload store - should regenerate embeddings
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore (reload) failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	if store.Count() != 1 {
		t.Errorf("expected 1 doc, got %d", store.Count())
	}

	// Search should work with regenerated embeddings
	query := relay.DeterministicEmbed("test content for embedding")
	results := store.Search(query, 1)

	if len(results) == 0 {
		t.Fatal("no results found after embedding regeneration")
	}

	if results[0].DocID != "doc1" {
		t.Errorf("expected doc1, got %s", results[0].DocID)
	}

	// Score should be high (exact match with regenerated embedding)
	if results[0].Score < 0.99 {
		t.Errorf("expected high score after regeneration, got %f", results[0].Score)
	}
}
