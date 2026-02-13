package httpapi

import "net/http"

// HandleHealth returns API health status and document count
func (h *Handler) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := HealthResponse{
		Status:   "healthy",
		DocCount: h.store.Count(),
	}

	h.logger.Debug().Int("doc_count", h.store.Count()).Msg("health check")

	writeJSON(w, http.StatusOK, resp)
}
