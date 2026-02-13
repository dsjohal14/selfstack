// Package main implements the HTTP API server for Selfstack.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	apihttp "github.com/dsjohal14/selfstack/internal/http"
	"github.com/dsjohal14/selfstack/internal/libs/config"
	"github.com/dsjohal14/selfstack/internal/libs/obs"
	"github.com/dsjohal14/selfstack/internal/scope/db"
	"github.com/dsjohal14/selfstack/internal/scope/db/wal"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
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

	// Initialize storage based on mode
	// WAL + Postgres is the default mode for production durability
	// Set WAL_DISABLED=true to use legacy in-memory store
	var store db.Storage
	walDisabled := strings.ToLower(os.Getenv("WAL_DISABLED")) == "true"
	dbConnString := os.Getenv("DATABASE_URL")

	if walDisabled {
		logger.Info().Msg("WAL disabled, using legacy store")
		store, err = db.NewStore(dataDir)
	} else {
		store, err = initWALStore(dataDir, dbConnString, logger)
	}
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

// initWALStore creates a WAL-backed store with optional Postgres manifest
func initWALStore(dataDir, dbConnString string, logger zerolog.Logger) (*db.WALStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config := db.DefaultWALStoreConfig(dataDir)

	// Connect to Postgres if configured
	if dbConnString != "" {
		pool, err := pgxpool.New(ctx, dbConnString)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}

		// Test connection
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, fmt.Errorf("failed to ping database: %w", err)
		}

		config.DB = pool
		// Compaction is enabled by default when Postgres is available
		// Set WAL_COMPACTION=false to disable
		config.EnableCompaction = strings.ToLower(os.Getenv("WAL_COMPACTION")) != "false"

		logger.Info().
			Bool("compaction", config.EnableCompaction).
			Msg("using Postgres-backed WAL manifest")
	} else {
		logger.Info().Msg("using in-memory WAL manifest (no Postgres)")
	}

	// Configure sync policy
	if strings.ToLower(os.Getenv("WAL_SYNC_IMMEDIATE")) == "false" {
		config.SyncPolicy = wal.DefaultSyncPolicy()
		logger.Info().Msg("using batched WAL sync policy")
	} else {
		config.SyncPolicy = wal.ImmediateSyncPolicy()
		logger.Info().Msg("using immediate WAL sync policy")
	}

	logger.Info().Str("wal_dir", config.WALDir).Msg("initializing WAL store")

	store, err := db.NewWALStore(ctx, config)
	if err != nil {
		return nil, err
	}

	logger.Info().Int("doc_count", store.Count()).Msg("WAL store initialized")
	return store, nil
}
