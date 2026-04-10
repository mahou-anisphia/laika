package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"laika/internal/domain"
	"laika/pkg/logger"
)

func WriteError(w http.ResponseWriter, r *http.Request, base *slog.Logger, err error) {
	log := logger.FromContext(r.Context(), base)

	var status int
	var message string

	switch {
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, "not found"
	case errors.Is(err, domain.ErrConflict):
		status, message = http.StatusConflict, "conflict"
	case errors.Is(err, domain.ErrForbidden):
		status, message = http.StatusForbidden, "forbidden"
	case errors.Is(err, domain.ErrBadRequest):
		status, message = http.StatusBadRequest, "bad request"
	default:
		// Unknown error — log the real cause, return generic 500
		log.Error("unhandled error", "error", err)
		status, message = http.StatusInternalServerError, "internal server error"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
