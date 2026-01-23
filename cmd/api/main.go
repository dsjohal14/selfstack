// Package main implements the HTTP API server for Selfstack.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	apihttp "github.com/dsjohal14/selfstack/internal/http"
	"github.com/dsjohal14/selfstack/internal/libs/config"
	"github.com/dsjohal14/selfstack/internal/libs/obs"
	"github.com/dsjohal14/selfstack/internal/scope/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Init logger
	obs.InitLogger(cfg.LogLevel)
	logger := obs.Logger("api")

	// Create data directory
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(".", "data")
	}

	// Initialize store
	store, err := db.NewStore(dataDir)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialize store")
	}
	defer func() { _ = store.Close() }()

	// Create HTTP handler
	handler := apihttp.NewHandler(store, logger)

	// Setup router
	r := setupRouter(handler)

	// Start server
	addr := fmt.Sprintf("%s:%s", cfg.APIHost, cfg.APIPort)
	logger.Info().Str("addr", addr).Msg("starting API server")

	if err := http.ListenAndServe(addr, r); err != nil {
		logger.Fatal().Err(err).Msg("server failed")
	}
}

func setupRouter(h *apihttp.Handler) *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	// Routes
	r.Get("/health", h.HandleHealth)
	r.Post("/ingest", h.HandleIngest)
	r.Post("/search", h.HandleSearch)
	r.Post("/run", h.HandleRun)

	return r
}
