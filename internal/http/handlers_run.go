package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dsjohal14/selfstack/internal/relay"
)

// HandleRun executes an AI agent query with citations
// Searches for relevant documents and composes an answer with source attribution
func (h *Handler) HandleRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn().Err(err).Msg("invalid run request")
		writeError(w, http.StatusBadRequest, "invalid JSON", "INVALID_JSON")
		return
	}

	// Validate query
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required", "MISSING_QUERY")
		return
	}

	// Search for relevant documents (top 3 for MVP)
	queryEmb := relay.DeterministicEmbed(req.Query)
	storeResults := h.store.Search(queryEmb, 3)

	// Convert to citations with source attribution
	citations := make([]Citation, len(storeResults))
	for i, r := range storeResults {
		citations[i] = Citation{
			DocID:  r.DocID,
			Score:  r.Score,
			Title:  r.Title,
			Text:   r.Text,
			Source: r.Source,
		}
	}

	// Compose answer from citations (AI layer logic)
	answer := composeAnswer(req.Query, citations)

	h.logger.Info().
		Str("query", req.Query).
		Int("citations", len(citations)).
		Msg("agent run completed")

	writeJSON(w, http.StatusOK, RunResponse{
		Answer:    answer,
		Citations: citations,
		Query:     req.Query,
	})
}

// composeAnswer creates a simple answer from citations
// TODO: Replace with real LLM-based answer generation in V1
func composeAnswer(query string, citations []Citation) string {
	if len(citations) == 0 {
		return fmt.Sprintf("No relevant documents found for query: %s", query)
	}

	answer := fmt.Sprintf("Based on %d document(s):\n\n", len(citations))

	for i, cit := range citations {
		// Truncate text to first 100 chars for summary
		text := cit.Text
		if len(text) > 100 {
			text = text[:100] + "..."
		}

		answer += fmt.Sprintf("%d. [%s] %s (score: %.3f)\n   %s\n\n",
			i+1, cit.Source, cit.Title, cit.Score, text)
	}

	return answer
}
