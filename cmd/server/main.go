package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"laika/internal/config"
	"laika/internal/middleware"
	"laika/internal/modules/email"
	"laika/internal/modules/health"
	"laika/internal/provider"
	"laika/pkg/logger"
)

func main() {
	base := logger.New()

	cfg := config.Load()

	// Build one Service per named email flow. To add a new flow, add its
	// SMTP_<FLOW>_* env vars and a matching entry in cfg.EmailFlows.
	flows := make(map[string]*email.Service, len(cfg.EmailFlows))
	for name, smtpCfg := range cfg.EmailFlows {
		p := provider.NewSMTPProvider(smtpCfg)
		flows[name] = email.NewService(p, smtpCfg.From, base)
	}
	emailRegistry := email.NewRegistry(flows)

	healthHdl := &health.Handler{}
	emailHdl := email.NewHandler(emailRegistry, base)

	r := chi.NewRouter()

	// Middleware — order is significant
	r.Use(middleware.RequestID)      // 1. assign/propagate X-Request-ID
	r.Use(middleware.Recovery(base)) // 2. catch panics before logger writes
	r.Use(middleware.Logger(base))   // 3. log after recovery so status is accurate

	r.Route("/api", func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/health", healthHdl.Check)
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"message":"hello world"}`))
			})
			r.Post("/noti/email/{flow}", emailHdl.SendEmail)
		})
	})

	addr := ":" + cfg.Port
	base.Info("server starting", "port", cfg.Port)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}
