package config

import "os"

// Config is the single root config loaded at startup. Add new sub-configs here
// as the service grows; their struct definitions live in their own files.
type Config struct {
	SMTP SMTP
}

func Load() Config {
	return Config{
		SMTP: SMTP{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     os.Getenv("SMTP_PORT"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     os.Getenv("SMTP_FROM"),
		},
	}
}
