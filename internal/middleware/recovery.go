package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"laika/pkg/logger"
)

func Recovery(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log := logger.FromContext(r.Context(), base)
					log.Error("panic recovered",
						"error", rec,
						"stack", string(debug.Stack()),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"error": "internal server error",
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
