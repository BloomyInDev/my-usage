package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccountsDir != "accounts" || cfg.Listen != ":9090" || cfg.PollInterval != 5*time.Minute {
		t.Fatalf("unexpected defaults %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"MY_USAGE_ACCOUNTS_DIR":  "/accounts",
		"MY_USAGE_POLL_INTERVAL": "90s",
		"MY_USAGE_LOG_FORMAT":    "json",
		"MY_USAGE_LISTEN":        "", // empty keeps the default
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccountsDir != "/accounts" || cfg.PollInterval != 90*time.Second ||
		cfg.LogFormat != "json" || cfg.Listen != ":9090" {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestLoadRejects(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"bad duration":  {"MY_USAGE_POLL_INTERVAL": "soon"},
		"zero poll":     {"MY_USAGE_POLL_INTERVAL": "0s"},
		"negative scan": {"MY_USAGE_RESCAN_INTERVAL": "-1m"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(env(m)); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}
