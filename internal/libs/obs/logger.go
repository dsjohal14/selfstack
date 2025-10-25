package obs

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitLogger initializes the global logger
func InitLogger(level string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Parse log level
	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		logLevel = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(logLevel)

	// Pretty print in development
	if os.Getenv("ENV") == "dev" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
}

// Logger returns a new logger with the given component name
func Logger(component string) zerolog.Logger {
	return log.With().Str("component", component).Logger()
}

