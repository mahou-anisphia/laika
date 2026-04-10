package logger

import (
	"context"
	"log/slog"
	"os"
	"time"

	"laika/internal/reqctx"
)

func New() *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// ISO 8601 timestamp
			if a.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, a.Value.Time().UTC().Format(time.RFC3339))
			}
			return a
		},
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

// FromContext returns a logger pre-seeded with the request ID from context.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if id := reqctx.GetRequestID(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}
