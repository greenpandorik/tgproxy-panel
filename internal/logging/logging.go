// Package logging configures slog and provides a Redacted value type.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Redacted wraps a secret so it never appears in logs.
type Redacted string

func (Redacted) LogValue() slog.Value { return slog.StringValue("[redacted]") }
func (Redacted) String() string       { return "[redacted]" }

func New(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
