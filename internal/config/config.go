package config

import (
	"fmt"
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

// Load reads configuration from environment variables and validates required
// fields. Returns an error listing every missing required var so the operator
// fixes them in one pass instead of re-running after each.
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

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate is the single place to declare which env vars must be set for the
// process to start. Add new required vars here. SMTP flows are intentionally
// not validated — a flow with empty creds fails at request time, not at boot,
// so a Discord-only deployment can still start.
func (c Config) validate() error {
	var missing []string
	if c.Discord.BotToken == "" {
		missing = append(missing, "DISCORD_BOT_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return nil
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
