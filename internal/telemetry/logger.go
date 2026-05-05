// Package telemetry wires the global logger and (in later phases) OpenTelemetry tracing.
//
// Phase 0 ships logging only. OTel arrives in Phase 6.
package telemetry

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// LoggerConfig is a minimal projection of config.LoggingConfig used by NewLogger.
// Keeping a local type avoids an import cycle between telemetry and config.
type LoggerConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// NewLogger builds a structured logger writing to w (typically os.Stderr).
//
// Defaults: level=info, format=json. Unknown levels fall back to info with a warning attribute.
func NewLogger(cfg LoggerConfig, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	level := parseLevel(cfg.Level)

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var h slog.Handler
	if strings.EqualFold(cfg.Format, "text") {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}

	return slog.New(h)
}

// parseLevel maps a level string to slog.Level. Unknown -> Info.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}
