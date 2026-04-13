package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type Handler struct {
	DB *sql.DB
	// add other deps here: RedisClient, ExternalAPIURL, etc.
}

type status struct {
	Status string            `json:"status"`  // "ok" | "degraded"
	Checks map[string]string `json:"checks"`  // dep name → "ok" | "error: ..."
	Time   string            `json:"time"`
}

func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := map[string]string{}
	overall := "ok"

	// Database ping — skipped until a *sql.DB is wired in via main.go
	if h.DB != nil {
		if err := h.DB.PingContext(ctx); err != nil {
			checks["database"] = "error: " + err.Error()
			overall = "degraded"
		} else {
			checks["database"] = "ok"
		}
	}

	// Add more dependency checks here, e.g. Redis:
	// if err := h.Redis.Ping(ctx).Err(); err != nil {
	//     checks["redis"] = "error: " + err.Error()
	//     overall = "degraded"
	// } else {
	//     checks["redis"] = "ok"
	// }

	code := http.StatusOK
	if overall == "degraded" {
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status{
		Status: overall,
		Checks: checks,
		Time:   time.Now().UTC().Format(time.RFC3339),
	})
}
