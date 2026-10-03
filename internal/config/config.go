// Package config loads the YAML configuration file.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	AccountsDir    string        `yaml:"accounts_dir"`
	Listen         string        `yaml:"listen"`
	PollInterval   time.Duration `yaml:"poll_interval"`
	RescanInterval time.Duration `yaml:"rescan_interval"`
	LogFormat      string        `yaml:"log_format"` // "text" or "json"
	LogLevel       string        `yaml:"log_level"`  // "debug", "info", "warn", "error"
}

func Defaults() Config {
	return Config{
		AccountsDir:    "accounts",
		Listen:         ":9090",
		PollInterval:   5 * time.Minute,
		RescanInterval: time.Minute,
		LogFormat:      "text",
		LogLevel:       "info",
	}
}

// Load reads the file over the defaults. A missing file is an error only when
// required, i.e. when the operator named it.
func Load(path string, required bool) (Config, error) {
	cfg := Defaults()
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist) && !required:
		return cfg, cfg.validate()
	case err != nil:
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	switch {
	case c.AccountsDir == "":
		return errors.New("config: accounts_dir is empty")
	case c.Listen == "":
		return errors.New("config: listen is empty")
	case c.PollInterval <= 0:
		return errors.New("config: poll_interval must be positive")
	case c.RescanInterval <= 0:
		return errors.New("config: rescan_interval must be positive")
	}
	return nil
}
