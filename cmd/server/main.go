package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"laika/internal/handler"
	"laika/internal/middleware"
	"laika/pkg/logger"
)

func main() {
	base := logger.New()

	healthHdl := &handler.HealthHandler{}

	r := chi.NewRouter()

	// Middleware — order is significant
	r.Use(middleware.RequestID)         // 1. assign/propagate X-Request-ID
	r.Use(middleware.Recovery(base))    // 2. catch panics before logger writes
	r.Use(middleware.Logger(base))      // 3. log after recovery so status is accurate

	r.Get("/health", healthHdl.Check)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"hello world"}`))
	})

	base.Info("server starting", "port", "8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatal(err)
	}
}
