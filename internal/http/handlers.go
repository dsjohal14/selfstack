package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/dsjohal14/selfstack/internal/scope/db"
	"github.com/rs/zerolog"
)

// Handler contains HTTP handlers for the API
type Handler struct {
	store  *db.Store
	logger zerolog.Logger
}

// NewHandler creates a new HTTP handler
func NewHandler(store *db.Store, logger zerolog.Logger) *Handler {
	return &Handler{
		store:  store,
		logger: logger,
	}
}

// Helper functions used across all handlers

// writeJSON writes a JSON response with the given status code
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes an error response with the given status code
func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}
