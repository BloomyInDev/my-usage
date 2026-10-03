// Package config reads the process configuration from environment variables.
package config

import (
	"fmt"
	"time"
)

type Config struct {
	AccountsDir    string
	Listen         string
	PollInterval   time.Duration
	RescanInterval time.Duration
	LogFormat      string // "text" or "json"
	LogLevel       string // "debug", "info", "warn", "error"
}

// Load reads MY_USAGE_* variables through getenv (os.Getenv in production).
// An unset or empty variable keeps its default; a malformed one is an error
// naming it.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		AccountsDir:    "accounts",
		Listen:         ":9090",
		PollInterval:   5 * time.Minute,
		RescanInterval: time.Minute,
		LogFormat:      "text",
		LogLevel:       "info",
	}
	str := func(name string, dst *string) {
		if v := getenv(name); v != "" {
			*dst = v
		}
	}
	var errs []error
	dur := func(name string, dst *time.Duration) {
		v := getenv(name)
		if v == "" {
			return
		}
		d, err := time.ParseDuration(v)
		if err == nil && d <= 0 {
			err = fmt.Errorf("must be positive")
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: %w", name, v, err))
			return
		}
		*dst = d
	}

	str("MY_USAGE_ACCOUNTS_DIR", &cfg.AccountsDir)
	str("MY_USAGE_LISTEN", &cfg.Listen)
	dur("MY_USAGE_POLL_INTERVAL", &cfg.PollInterval)
	dur("MY_USAGE_RESCAN_INTERVAL", &cfg.RescanInterval)
	str("MY_USAGE_LOG_FORMAT", &cfg.LogFormat)
	str("MY_USAGE_LOG_LEVEL", &cfg.LogLevel)

	if len(errs) > 0 {
		return Config{}, errs[0]
	}
	return cfg, nil
}
