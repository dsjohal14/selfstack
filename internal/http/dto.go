// Package httpapi provides HTTP handlers and data transfer objects for the Selfstack API.
package httpapi

import "time"

// HealthResponse represents the health check response
type HealthResponse struct {
	Status   string `json:"status"`
	DocCount int    `json:"doc_count"`
}

// IngestRequest represents document ingestion request
// Maps to the Doc contract schema
type IngestRequest struct {
	ID        string            `json:"id"`     // UUID format
	Source    string            `json:"source"` // Source identifier
	Title     string            `json:"title"`  // Document title
	Text      string            `json:"text"`   // Full text content
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"` // Auto-set if not provided
}

// IngestResponse represents ingestion response
type IngestResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// SearchRequest represents search request
type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"` // Default: 10
}

// SearchResult represents a single search result with score
type SearchResult struct {
	DocID     string            `json:"doc_id"`
	Score     float32           `json:"score"`
	Title     string            `json:"title"`
	Text      string            `json:"text"`
	Source    string            `json:"source"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// SearchResponse represents search results
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Count   int            `json:"count"`
	Query   string         `json:"query"`
}

// RunRequest represents agent run request
type RunRequest struct {
	Query string `json:"query"`
}

// Citation represents a cited document in the answer
type Citation struct {
	DocID     string  `json:"doc_id"`
	Score     float32 `json:"score"`
	Title     string  `json:"title"`
	Text      string  `json:"text"`
	Source    string  `json:"source"`
	Relevance string  `json:"relevance,omitempty"` // Why this doc was cited
}

// RunResponse represents agent response with citations
type RunResponse struct {
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
	Query     string     `json:"query"`
}

// ErrorResponse represents API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}
