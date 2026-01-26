package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dsjohal14/selfstack/internal/relay"
	"github.com/dsjohal14/selfstack/internal/scope/db"
)

// HandleIngest ingests a new document into the system
// Validates required fields per Doc contract schema
func (h *Handler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	var req IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Msg("invalid ingest request")
		writeError(w, http.StatusBadRequest, "invalid JSON", "INVALID_JSON")
		return
	}

	// Validate required fields per Doc contract
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required", "MISSING_ID")
		return
	}
	if req.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required", "MISSING_SOURCE")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required", "MISSING_TITLE")
		return
	}
	if req.Text == "" {
		req.Text = req.Title // Use title as text if empty
	}

	// Set created_at if not provided
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now()
	}

	// Generate embedding from text (AI layer - relay)
	embedding := relay.DeterministicEmbed(req.Text)

	// Create document
	doc := db.Document{
		ID:        req.ID,
		Source:    req.Source,
		Title:     req.Title,
		Text:      req.Text,
		Metadata:  req.Metadata,
		CreatedAt: req.CreatedAt,
		Embedding: embedding,
	}

	// Store document (WAL handles durability based on sync policy:
	// - Immediate sync: fsyncs after every write, durable when Add returns
	// - Batched sync: background fsync for throughput, may lose recent writes on crash)
	if err := h.store.Add(doc); err != nil {
		h.logger.Error().Err(err).Str("doc_id", req.ID).Msg("failed to store document")
		writeError(w, http.StatusInternalServerError, "failed to store document", "STORE_ERROR")
		return
	}

	h.logger.Info().
		Str("doc_id", req.ID).
		Str("source", req.Source).
		Str("title", req.Title).
		Msg("document ingested")

	writeJSON(w, http.StatusOK, IngestResponse{
		ID:      req.ID,
		Success: true,
		Message: "document ingested successfully",
	})
}
