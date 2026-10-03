package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadOverridesDefaults(t *testing.T) {
	cfg, err := Load(write(t, "poll_interval: 90s\nlisten: \":1234\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 90*time.Second || cfg.Listen != ":1234" || cfg.AccountsDir != "accounts" {
		t.Fatalf("unexpected config %+v", cfg)
	}
}

func TestLoadMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if _, err := Load(missing, false); err != nil {
		t.Fatalf("optional missing file: %v", err)
	}
	if _, err := Load(missing, true); err == nil {
		t.Fatal("required missing file must fail")
	}
}

func TestLoadRejects(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":  "polll_interval: 1m\n",
		"bad duration": "poll_interval: soon\n",
		"zero poll":    "poll_interval: 0s\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body), true); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestLoadEmptyFile(t *testing.T) {
	if _, err := Load(write(t, ""), true); err != nil {
		t.Fatalf("empty file should keep defaults: %v", err)
	}
}
