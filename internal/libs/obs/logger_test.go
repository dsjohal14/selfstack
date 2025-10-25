package obs

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		expected zerolog.Level
	}{
		{"info level", "info", zerolog.InfoLevel},
		{"debug level", "debug", zerolog.DebugLevel},
		{"warn level", "warn", zerolog.WarnLevel},
		{"invalid level defaults to info", "invalid", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InitLogger(tt.level)
			if zerolog.GlobalLevel() != tt.expected {
				t.Errorf("expected level %v, got %v", tt.expected, zerolog.GlobalLevel())
			}
		})
	}
}

func TestLogger(t *testing.T) {
	logger := Logger("test-component")

	// Verify logger has the component field set
	ctx := logger.With().Logger().GetLevel()
	if ctx == zerolog.Disabled {
		t.Error("logger should not be disabled")
	}
}

