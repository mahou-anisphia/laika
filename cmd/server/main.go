package main

import (
	"encoding/json"
	"errors"
	"log"
	"log/slog"
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

	emailRegistry := setupEmail(base, cfg.EmailFlows)
	discordHdl, discordStatus, closeDiscord := setupDiscord(base, cfg.Discord)
	defer closeDiscord()

	logBootStatus(base, emailRegistry.Statuses(), discordStatus)

	emailHdl := email.NewHandler(emailRegistry, base)
	healthHdl := &health.Handler{
		Email:   componentStatusFromFlows(emailRegistry.Statuses()),
		Discord: discordStatus,
	}

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
					// Email gateway. Always registered — per-flow availability
					// is surfaced via /health, not by hiding the route.
					r.Post("/email/{flow}", emailHdl.SendEmail)

					// Discord gateway. Routes are always registered; when the
					// module is degraded they short-circuit to 503 with the
					// boot-time reason so callers see "module degraded"
					// instead of a generic 404.
					r.Route("/discord", func(r chi.Router) {
						if discordHdl != nil {
							r.Get("/servers", discordHdl.ListServers)
							r.Get("/servers/{id}/channels", discordHdl.ListChannels)
							r.Post("/channels/{id}/messages", discordHdl.SendMessage)
						} else {
							degraded := discordDegradedHandler(discordStatus.Reason)
							r.Get("/servers", degraded)
							r.Get("/servers/{id}/channels", degraded)
							r.Post("/channels/{id}/messages", degraded)
						}
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

// setupEmail builds one Service per named flow and probes each for usable
// creds. A failed probe is logged and the flow stays registered but marked
// unavailable in /health. Requests to a degraded flow still hit the dial path
// and fail with a structured 5xx — we don't short-circuit at the handler.
func setupEmail(base *slog.Logger, flowCfgs map[string]config.SMTP) *email.Registry {
	flows := make(map[string]*email.Service, len(flowCfgs))
	for name, smtpCfg := range flowCfgs {
		p := provider.NewSMTPProvider(smtpCfg)
		flows[name] = email.NewService(p, smtpCfg.From, base)
	}
	registry := email.NewRegistry(flows)

	for name, smtpCfg := range flowCfgs {
		p := provider.NewSMTPProvider(smtpCfg)
		perr := p.Probe()
		if perr == nil {
			base.Info("email flow ready", "flow", name, "host", smtpCfg.Host)
			registry.SetStatus(name, email.FlowStatus{Available: true})
			continue
		}
		if errors.Is(perr, provider.ErrSMTPNotConfigured) {
			base.Warn("email flow not configured, skipping", "flow", name)
		} else {
			base.Warn("email flow probe failed", "flow", name, "error", perr)
		}
		registry.SetStatus(name, email.FlowStatus{Reason: perr.Error()})
	}
	return registry
}

// setupDiscord opens the gateway if a token is set. Missing token or open
// failure is non-fatal: we log a warning, return a nil handler, and the caller
// skips route registration. The returned closer is always safe to defer.
func setupDiscord(base *slog.Logger, cfg config.Discord) (*discord.Handler, health.ComponentStatus, func()) {
	noop := func() {
		// No session was opened, so there's nothing to close. Returned so the
		// caller can `defer` unconditionally regardless of branch taken below.
	}
	if cfg.BotToken == "" {
		base.Warn("discord not configured, skipping", "reason", "DISCORD_BOT_TOKEN is empty")
		return nil, health.ComponentStatus{Reason: "DISCORD_BOT_TOKEN is empty"}, noop
	}

	sess, err := provider.NewDiscordSession(cfg)
	if err != nil {
		base.Warn("discord session failed, continuing without it", "error", err)
		return nil, health.ComponentStatus{Reason: err.Error()}, noop
	}

	base.Info("discord ready")
	svc := discord.NewService(sess, base)
	return discord.NewHandler(svc, base),
		health.ComponentStatus{Available: true},
		func() { _ = sess.Close() }
}

// discordDegradedHandler returns a handler that responds 503 with the boot
// probe's failure reason. Used when the gateway didn't come up, so callers
// learn the module is unusable instead of getting a confusing 404.
func discordDegradedHandler(reason string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "discord module degraded",
			"reason": reason,
		})
	}
}

// componentStatusFromFlows adapts the email registry's status map to the
// health handler's component type. Kept here rather than in either package to
// avoid forcing one to know about the other.
func componentStatusFromFlows(statuses map[string]email.FlowStatus) map[string]health.ComponentStatus {
	out := make(map[string]health.ComponentStatus, len(statuses))
	for name, s := range statuses {
		out[name] = health.ComponentStatus{Available: s.Available, Reason: s.Reason}
	}
	return out
}

// logBootStatus emits one operator-friendly line summarising the boot probes.
// Info if everything came up, Warn if anything is degraded, Error if nothing
// is available (the process will still serve /health and the hello-world
// endpoint, but no real work can be done).
func logBootStatus(l *slog.Logger, emailStatuses map[string]email.FlowStatus, discord health.ComponentStatus) {
	available, total := 0, 0
	degraded := []string{}

	for name, s := range emailStatuses {
		total++
		if s.Available {
			available++
		} else {
			degraded = append(degraded, "email/"+name)
		}
	}
	total++
	if discord.Available {
		available++
	} else {
		degraded = append(degraded, "discord")
	}

	switch {
	case available == total:
		l.Info("boot status: all modules ready")
	case available == 0:
		l.Error("boot status: DOWN — no modules available", "degraded", degraded)
	default:
		l.Warn("boot status: degraded", "available", available, "total", total, "degraded", degraded)
	}
}
