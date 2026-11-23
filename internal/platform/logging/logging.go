package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New builds the application logger: JSON in production/staging, text
// elsewhere. Debug level is forced when debug or verbose is set.
func New(env, level string, debug bool, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}
	opts := &slog.HandlerOptions{Level: parseLevel(level, debug)}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "production", "staging":
		handler = slog.NewJSONHandler(out, opts)
	default:
		handler = slog.NewTextHandler(out, opts)
	}
	return slog.New(handler)
}

func parseLevel(level string, debug bool) slog.Level {
	if debug {
		return slog.LevelDebug
	}
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
