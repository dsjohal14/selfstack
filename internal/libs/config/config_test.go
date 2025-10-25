package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Test with default values
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.APIPort != "8080" {
		t.Errorf("expected default APIPort=8080, got %s", cfg.APIPort)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("expected default LogLevel=info, got %s", cfg.LogLevel)
	}
}

func TestLoadWithEnv(t *testing.T) {
	// Test with environment variables
	_ = os.Setenv("API_PORT", "9000")
	_ = os.Setenv("LOG_LEVEL", "debug")
	defer func() {
		_ = os.Unsetenv("API_PORT")
		_ = os.Unsetenv("LOG_LEVEL")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.APIPort != "9000" {
		t.Errorf("expected APIPort=9000, got %s", cfg.APIPort)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel=debug, got %s", cfg.LogLevel)
	}
}
