package db

import (
	"context"
	"testing"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
	"github.com/dsjohal14/selfstack/internal/scope/db/wal"
)

func TestNewWALStore(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	store, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}
	defer store.Close()

	if store.Count() != 0 {
		t.Errorf("expected 0 documents, got %d", store.Count())
	}
}

func TestWALStoreAdd(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	store, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}
	defer store.Close()

	// Add document
	doc := Document{
		ID:        "test-doc-1",
		Source:    "test",
		Title:     "Test Document",
		Text:      "This is a test document",
		Metadata:  map[string]string{"key": "value"},
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("This is a test document"),
	}

	err = store.Add(doc)
	if err != nil {
		t.Fatalf("failed to add document: %v", err)
	}

	if store.Count() != 1 {
		t.Errorf("expected 1 document, got %d", store.Count())
	}

	// Retrieve document
	retrieved, found := store.Get("test-doc-1")
	if !found {
		t.Fatal("document not found")
	}
	if retrieved.Title != doc.Title {
		t.Errorf("title mismatch: expected %q, got %q", doc.Title, retrieved.Title)
	}
}

func TestWALStoreDelete(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	store, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}
	defer store.Close()

	// Add then delete
	doc := Document{
		ID:        "test-doc-1",
		Source:    "test",
		Title:     "Test Document",
		Text:      "test",
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("test"),
	}
	store.Add(doc)

	if store.Count() != 1 {
		t.Errorf("expected 1 document after add")
	}

	err = store.Delete("test-doc-1")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	if store.Count() != 0 {
		t.Errorf("expected 0 documents after delete, got %d", store.Count())
	}

	_, found := store.Get("test-doc-1")
	if found {
		t.Error("document should not be found after delete")
	}
}

func TestWALStoreSearch(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	store, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}
	defer store.Close()

	// Add documents
	docs := []Document{
		{ID: "doc1", Source: "test", Title: "Hello World", Text: "Hello world document", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("Hello world document")},
		{ID: "doc2", Source: "test", Title: "Goodbye World", Text: "Goodbye world document", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("Goodbye world document")},
		{ID: "doc3", Source: "test", Title: "Different Topic", Text: "Something completely different", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("Something completely different")},
	}

	for _, doc := range docs {
		if err := store.Add(doc); err != nil {
			t.Fatalf("failed to add document: %v", err)
		}
	}

	// Search
	query := relay.DeterministicEmbed("hello")
	results := store.Search(query, 10)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Results should be sorted by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted by score")
		}
	}
}

func TestWALStoreRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Create store and add documents
	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	store1, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}

	for i := 0; i < 10; i++ {
		doc := Document{
			ID:        string(rune('a' + i)),
			Source:    "test",
			Title:     "Doc " + string(rune('a'+i)),
			Text:      "text " + string(rune('a'+i)),
			CreatedAt: time.Now(),
			Embedding: relay.DeterministicEmbed("text " + string(rune('a'+i))),
		}
		if err := store1.Add(doc); err != nil {
			t.Fatalf("failed to add document: %v", err)
		}
	}

	countBefore := store1.Count()
	store1.Close()

	// Reopen store - should recover
	store2, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to reopen WAL store: %v", err)
	}
	defer store2.Close()

	if store2.Count() != countBefore {
		t.Errorf("expected %d documents after recovery, got %d", countBefore, store2.Count())
	}

	// Verify a document
	doc, found := store2.Get("a")
	if !found {
		t.Error("document 'a' not found after recovery")
	}
	if doc.Title != "Doc a" {
		t.Errorf("title mismatch after recovery: expected 'Doc a', got %q", doc.Title)
	}
}

func TestWALStoreRecoveryWithDelete(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	// Create and add documents
	store1, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}

	store1.Add(Document{ID: "doc1", Source: "test", Title: "Doc 1", Text: "text1", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("text1")})
	store1.Add(Document{ID: "doc2", Source: "test", Title: "Doc 2", Text: "text2", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("text2")})
	store1.Add(Document{ID: "doc3", Source: "test", Title: "Doc 3", Text: "text3", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("text3")})

	// Delete one
	store1.Delete("doc2")
	store1.Close()

	// Reopen
	store2, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to reopen WAL store: %v", err)
	}
	defer store2.Close()

	if store2.Count() != 2 {
		t.Errorf("expected 2 documents after recovery, got %d", store2.Count())
	}

	_, found := store2.Get("doc2")
	if found {
		t.Error("deleted document should not be found after recovery")
	}

	_, found = store2.Get("doc1")
	if !found {
		t.Error("doc1 should be found")
	}

	_, found = store2.Get("doc3")
	if !found {
		t.Error("doc3 should be found")
	}
}

func TestWALStoreUpdate(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	store, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}
	defer store.Close()

	// Add document
	doc := Document{
		ID:        "doc1",
		Source:    "test",
		Title:     "Original Title",
		Text:      "original text",
		CreatedAt: time.Now(),
		Embedding: relay.DeterministicEmbed("original text"),
	}
	store.Add(doc)

	// Update document
	doc.Title = "Updated Title"
	doc.Text = "updated text"
	doc.Embedding = relay.DeterministicEmbed("updated text")
	store.Add(doc)

	if store.Count() != 1 {
		t.Errorf("expected 1 document after update, got %d", store.Count())
	}

	// Verify update
	retrieved, _ := store.Get("doc1")
	if retrieved.Title != "Updated Title" {
		t.Errorf("title not updated: got %q", retrieved.Title)
	}
}

func TestWALStoreUpdateRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)
	config.SyncPolicy = wal.ImmediateSyncPolicy()

	// Create, add, and update
	store1, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}

	doc := Document{ID: "doc1", Source: "test", Title: "V1", Text: "text", CreatedAt: time.Now(), Embedding: relay.DeterministicEmbed("text")}
	store1.Add(doc)

	doc.Title = "V2"
	store1.Add(doc)

	doc.Title = "V3"
	store1.Add(doc)

	store1.Close()

	// Reopen and verify latest version
	store2, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to reopen: %v", err)
	}
	defer store2.Close()

	retrieved, _ := store2.Get("doc1")
	if retrieved.Title != "V3" {
		t.Errorf("expected latest version V3, got %q", retrieved.Title)
	}
}

func TestWALStoreFlush(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	config := DefaultWALStoreConfig(dir)

	store, err := NewWALStore(ctx, config)
	if err != nil {
		t.Fatalf("failed to create WAL store: %v", err)
	}
	defer store.Close()

	// Flush should be a no-op (all writes are already synced)
	err = store.Flush()
	if err != nil {
		t.Errorf("flush failed: %v", err)
	}
}
