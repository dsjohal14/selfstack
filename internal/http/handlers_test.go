package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dsjohal14/selfstack/internal/libs/obs"
	"github.com/dsjohal14/selfstack/internal/scope/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func setupTestHandler(t *testing.T) (*Handler, *chi.Mux) {
	tmpDir := t.TempDir()

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	obs.InitLogger("error") // Quiet logs during tests
	logger := obs.Logger("test")
	handler := NewHandler(store, logger)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Get("/health", handler.HandleHealth)
	r.Post("/ingest", handler.HandleIngest)
	r.Post("/search", handler.HandleSearch)
	r.Post("/run", handler.HandleRun)

	return handler, r
}

func TestHandleHealth(t *testing.T) {
	_, router := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %v", resp.Status)
	}
}

func TestHandleIngest(t *testing.T) {
	_, router := setupTestHandler(t)

	reqBody := IngestRequest{
		ID:        "test-doc-1",
		Source:    "test",
		Title:     "Test Document",
		Text:      "This is a test document",
		CreatedAt: time.Now(),
		Metadata: map[string]string{
			"author": "test-user",
		},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp IngestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.ID != "test-doc-1" {
		t.Errorf("expected ID test-doc-1, got %s", resp.ID)
	}
}

func TestHandleSearch(t *testing.T) {
	_, router := setupTestHandler(t)

	// First, ingest a document
	ingestReq := IngestRequest{
		ID:        "search-doc-1",
		Source:    "test",
		Title:     "Machine Learning Basics",
		Text:      "machine learning algorithms and models",
		CreatedAt: time.Now(),
	}
	body, _ := json.Marshal(ingestReq)
	req := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)

	// Now search
	searchReq := SearchRequest{
		Query: "machine learning",
		Limit: 10,
	}
	body, _ = json.Marshal(searchReq)
	req = httptest.NewRequest(http.MethodPost, "/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Count == 0 {
		t.Error("expected at least 1 result")
	}

	if len(resp.Results) == 0 {
		t.Fatal("expected results array to have items")
	}

	if resp.Results[0].DocID != "search-doc-1" {
		t.Errorf("expected doc search-doc-1, got %s", resp.Results[0].DocID)
	}

	// Verify response includes all fields from contract
	if resp.Results[0].Title == "" {
		t.Error("expected title to be present")
	}
	if resp.Results[0].Source == "" {
		t.Error("expected source to be present")
	}
}

func TestHandleRun(t *testing.T) {
	_, router := setupTestHandler(t)

	// Ingest a document first
	ingestReq := IngestRequest{
		ID:        "run-doc-1",
		Source:    "test",
		Title:     "AI Technology",
		Text:      "Artificial intelligence is transforming technology",
		CreatedAt: time.Now(),
	}
	body, _ := json.Marshal(ingestReq)
	req := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)

	// Run agent query
	runReq := RunRequest{
		Query: "AI technology",
	}
	body, _ = json.Marshal(runReq)
	req = httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Answer == "" {
		t.Error("expected non-empty answer")
	}

	if len(resp.Citations) == 0 {
		t.Error("expected citations")
	}

	if resp.Citations[0].DocID != "run-doc-1" {
		t.Errorf("expected citation to run-doc-1, got %s", resp.Citations[0].DocID)
	}

	// Verify citation includes all required fields
	if resp.Citations[0].Title == "" {
		t.Error("expected citation title")
	}
	if resp.Citations[0].Source == "" {
		t.Error("expected citation source")
	}
}

// Black-box smoke test: ingest → search → run
func TestFullPipeline(t *testing.T) {
	_, router := setupTestHandler(t)

	// Step 1: Ingest two documents
	docs := []IngestRequest{
		{
			ID:        "doc1",
			Source:    "test",
			Title:     "Python Programming",
			Text:      "Python is a popular programming language",
			CreatedAt: time.Now(),
			Metadata:  map[string]string{"topic": "programming"},
		},
		{
			ID:        "doc2",
			Source:    "test",
			Title:     "Go Programming",
			Text:      "Go is designed for systems programming",
			CreatedAt: time.Now(),
			Metadata:  map[string]string{"topic": "programming"},
		},
	}

	for _, doc := range docs {
		body, _ := json.Marshal(doc)
		req := httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("ingest failed for %s: %d", doc.ID, w.Code)
		}
	}

	// Step 2: Search for "programming language"
	searchReq := SearchRequest{
		Query: "programming language",
		Limit: 10,
	}
	body, _ := json.Marshal(searchReq)
	req := httptest.NewRequest(http.MethodPost, "/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("search failed: %d", w.Code)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(w.Body).Decode(&searchResp); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}

	if searchResp.Count != 2 {
		t.Errorf("expected 2 search results, got %d", searchResp.Count)
	}

	// Verify both doc IDs are in results
	foundDoc1, foundDoc2 := false, false
	for _, result := range searchResp.Results {
		if result.DocID == "doc1" {
			foundDoc1 = true
		}
		if result.DocID == "doc2" {
			foundDoc2 = true
		}
	}

	if !foundDoc1 {
		t.Error("doc1 not found in search results")
	}
	if !foundDoc2 {
		t.Error("doc2 not found in search results")
	}

	// Step 3: Run agent query
	runReq := RunRequest{
		Query: "tell me about programming languages",
	}
	body, _ = json.Marshal(runReq)
	req = httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("run failed: %d", w.Code)
	}

	var runResp RunResponse
	if err := json.NewDecoder(w.Body).Decode(&runResp); err != nil {
		t.Fatalf("failed to decode run response: %v", err)
	}

	if runResp.Answer == "" {
		t.Error("expected non-empty answer")
	}

	if len(runResp.Citations) == 0 {
		t.Error("expected citations")
	}

	// Verify expected doc IDs in citations
	citedDoc1, citedDoc2 := false, false
	for _, citation := range runResp.Citations {
		if citation.DocID == "doc1" {
			citedDoc1 = true
		}
		if citation.DocID == "doc2" {
			citedDoc2 = true
		}
	}

	if !citedDoc1 && !citedDoc2 {
		t.Error("neither doc1 nor doc2 found in citations")
	}

	t.Logf("✅ Full pipeline test passed: ingest → search → run")
	t.Logf("   Answer length: %d chars", len(runResp.Answer))
	t.Logf("   Citations: %d", len(runResp.Citations))
}
