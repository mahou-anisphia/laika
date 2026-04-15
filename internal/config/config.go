package config

import (
	"os"
	"strings"
)

// Config is the single root config loaded at startup. Add new sub-configs here
// as the service grows; their struct definitions live in their own files.
type Config struct {
	Port       string
	EmailFlows map[string]SMTP
}

// Load reads configuration from environment variables. Each email flow is
// discovered by convention: SMTP_<FLOW>_HOST, SMTP_<FLOW>_PORT, etc.
// To add a new flow, add the corresponding env vars — no code changes needed.
func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	flows := map[string]SMTP{
		"corporate": loadSMTP("CORPORATE"),
		"personal":  loadSMTP("PERSONAL"),
	}

	return Config{
		Port:       port,
		EmailFlows: flows,
	}
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
