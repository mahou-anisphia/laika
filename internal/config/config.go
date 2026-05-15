package config

import (
	"os"
	"strconv"
	"strings"
)

// Config is the single root config loaded at startup. Add new sub-configs here
// as the service grows; their struct definitions live in their own files.
type Config struct {
	Port       string
	EmailFlows map[string]SMTP
	Discord    Discord
	RateLimit  RateLimit
}

// Load reads configuration from environment variables. Nothing is required at
// boot — both email and Discord are best-effort: missing or broken credentials
// surface as a degraded module via /health, not a refusal to start. This lets
// a single binary cover email-only, Discord-only, and full deployments.
//
// Each email flow is discovered by convention: SMTP_<FLOW>_HOST,
// SMTP_<FLOW>_PORT, etc. To add a new flow, add the corresponding env vars and
// register the key in the flows map below.
func Load() (Config, error) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	flows := map[string]SMTP{
		"corporate": loadSMTP("CORPORATE"),
		"personal":  loadSMTP("PERSONAL"),
	}

	cfg := Config{
		Port:       port,
		EmailFlows: flows,
		Discord: Discord{
			BotToken: os.Getenv("DISCORD_BOT_TOKEN"),
		},
		RateLimit: RateLimit{
			RPM:   getenvInt("LAIKA_RATELIMIT_RPM", 60),
			Burst: getenvInt("LAIKA_RATELIMIT_BURST", 30),
		},
	}

	return cfg, nil
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// loadSMTP reads SMTP_<PREFIX>_* env vars into an SMTP config.
func loadSMTP(prefix string) SMTP {
	p := "SMTP_" + strings.ToUpper(prefix) + "_"
	return SMTP{
		Host:     os.Getenv(p + "HOST"),
		Port:     os.Getenv(p + "PORT"),
		Username: os.Getenv(p + "USERNAME"),
		Password: os.Getenv(p + "PASSWORD"),
		From:     os.Getenv(p + "FROM"),
	}
}
