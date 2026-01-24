package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/dsjohal14/selfstack/internal/relay"
)

// HandleSearch performs semantic search over stored documents
// Uses embeddings to find documents similar to the query
func (h *Handler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Msg("invalid search request")
		writeError(w, http.StatusBadRequest, "invalid JSON", "INVALID_JSON")
		return
	}

	// Validate query
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required", "MISSING_QUERY")
		return
	}

	// Set default and max limits
	if req.Limit == 0 {
		req.Limit = 10 // Default limit
	}
	if req.Limit > 100 {
		req.Limit = 100 // Max limit for performance
	}

	// Generate query embedding (AI layer - relay)
	queryEmb := relay.DeterministicEmbed(req.Query)

	// Search via storage layer
	storeResults := h.store.Search(queryEmb, req.Limit)

	// Convert to API response format with all Doc contract fields
	results := make([]SearchResult, len(storeResults))
	for i, r := range storeResults {
		results[i] = SearchResult{
			DocID:     r.DocID,
			Score:     r.Score,
			Title:     r.Title,
			Text:      r.Text,
			Source:    r.Source,
			Metadata:  r.Metadata,
			CreatedAt: r.CreatedAt,
		}
	}

	h.logger.Info().
		Str("query", req.Query).
		Int("results", len(results)).
		Int("limit", req.Limit).
		Msg("search completed")

	writeJSON(w, http.StatusOK, SearchResponse{
		Results: results,
		Count:   len(results),
		Query:   req.Query,
	})
}
