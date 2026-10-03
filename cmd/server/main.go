package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"forge.red.bloomyindev.me/bastien/my-usage/internal/accounts"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/config"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/logging"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/metrics"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/provider"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/server"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/watcher"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to the YAML config file")
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "config" })

	cfg, err := config.Load(*configPath, explicit)
	if err != nil {
		return err
	}
	log, err := logging.New(os.Stderr, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry := provider.NewRegistry()
	store := metrics.NewStore()
	reg := prometheus.NewRegistry()
	reg.MustRegister(metrics.NewCollector(store, 2*cfg.PollInterval, time.Now))

	changed, err := watcher.Watch(ctx, cfg.AccountsDir, 200*time.Millisecond)
	if err != nil {
		return err
	}
	mgr := worker.New(worker.Config{
		Dir:            accounts.Dir(cfg.AccountsDir),
		Registry:       registry,
		Store:          store,
		PollInterval:   cfg.PollInterval,
		RescanInterval: cfg.RescanInterval,
		Now:            time.Now,
		Logger:         log,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		mgr.Run(ctx, changed)
	}()

	log.Info("listening", "addr", cfg.Listen, "accounts_dir", cfg.AccountsDir)
	err = server.Run(ctx, cfg.Listen, server.Handler(reg))
	stop()
	<-done
	return err
}
