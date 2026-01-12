package pkg

import (
	"log/slog"
	"os"
)

// Return slog.Logger object
func SetupLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := slog.New(handler)

	slog.SetDefault(logger)

	// No need & cuz logger is slog.New which returned *slog.Logger
	// no need &
	return logger
}
