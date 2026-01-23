// Package config provides application configuration management from environment variables.
package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
type Config struct {
	DatabaseURL string
	APIPort     string
	APIHost     string
	LogLevel    string
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://selfstack:selfstack@localhost:5432/selfstack?sslmode=disable"),
		APIPort:     getEnv("API_PORT", "8080"),
		APIHost:     getEnv("API_HOST", "0.0.0.0"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
