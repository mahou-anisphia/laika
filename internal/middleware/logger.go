package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"laika/pkg/logger"
)

func Logger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := newWrappedWriter(w)

			next.ServeHTTP(ww, r)

			log := logger.FromContext(r.Context(), base)
			log.Info("request",
				"method",     r.Method,
				"path",       r.URL.Path,
				"status",     ww.status,
				"latency_ms", time.Since(start).Milliseconds(),
				"remote_ip",  r.RemoteAddr,
			)
		})
	}
}

type wrappedWriter struct {
	http.ResponseWriter
	status int
}

func newWrappedWriter(w http.ResponseWriter) *wrappedWriter {
	return &wrappedWriter{ResponseWriter: w, status: http.StatusOK}
}

func (ww *wrappedWriter) WriteHeader(code int) {
	ww.status = code
	ww.ResponseWriter.WriteHeader(code)
}
