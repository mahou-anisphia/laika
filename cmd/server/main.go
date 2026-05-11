package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"

	"laika/internal/config"
	"laika/internal/middleware"
	"laika/internal/modules/discord"
	"laika/internal/modules/email"
	"laika/internal/modules/health"
	"laika/internal/provider"
	"laika/pkg/logger"
)

func main() {
	// Best-effort .env load for local dev (`make run`). Production runs in
	// Docker with --env-file, so a missing file here is expected and ignored.
	_ = godotenv.Load()

	base := logger.New()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Build one Service per named email flow. To add a new flow, add its
	// SMTP_<FLOW>_* env vars and a matching entry in cfg.EmailFlows.
	flows := make(map[string]*email.Service, len(cfg.EmailFlows))
	for name, smtpCfg := range cfg.EmailFlows {
		p := provider.NewSMTPProvider(smtpCfg)
		flows[name] = email.NewService(p, smtpCfg.From, base)
	}
	emailRegistry := email.NewRegistry(flows)

	// Discord gateway connection. Held open for the lifetime of the process so
	// the in-memory guild/channel cache stays warm.
	discordSess, err := provider.NewDiscordSession(cfg.Discord)
	if err != nil {
		log.Fatalf("discord session: %v", err)
	}
	defer discordSess.Close()

	healthHdl := &health.Handler{}
	emailHdl := email.NewHandler(emailRegistry, base)
	discordSvc := discord.NewService(discordSess, base)
	discordHdl := discord.NewHandler(discordSvc, base)

	r := chi.NewRouter()

	// Middleware — order is significant
	r.Use(middleware.RequestID)      // 1. assign/propagate X-Request-ID
	r.Use(middleware.Recovery(base)) // 2. catch panics before logger writes
	r.Use(middleware.Logger(base))   // 3. log after recovery so status is accurate

	r.Route("/api", func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			// Health stays outside the rate limiter — uptime probes hit it.
			r.Get("/health", healthHdl.Check)

			// Rate-limited surface. Auth is provided by Tailscale at the
			// network layer; the bucket here is self-protection only.
			r.Group(func(r chi.Router) {
				r.Use(middleware.RateLimit(base, cfg.RateLimit.RPM, cfg.RateLimit.Burst))

				r.Get("/", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"message":"hello world"}`))
				})

				r.Route("/noti", func(r chi.Router) {
					// Email gateway
					r.Post("/email/{flow}", emailHdl.SendEmail)

					// Discord gateway — operator browse endpoints + caller send
					r.Route("/discord", func(r chi.Router) {
						r.Get("/servers", discordHdl.ListServers)
						r.Get("/servers/{id}/channels", discordHdl.ListChannels)
						r.Post("/channels/{id}/messages", discordHdl.SendMessage)
					})
				})
			})
		})
	})

	addr := ":" + cfg.Port
	base.Info("server starting", "port", cfg.Port)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}
