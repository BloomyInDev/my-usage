package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BloomyInDev/my-usage/internal/accounts"
	"github.com/BloomyInDev/my-usage/internal/config"
	"github.com/BloomyInDev/my-usage/internal/logging"
	"github.com/BloomyInDev/my-usage/internal/metrics"
	"github.com/BloomyInDev/my-usage/internal/provider"
	"github.com/BloomyInDev/my-usage/internal/server"
	"github.com/BloomyInDev/my-usage/internal/watcher"
	"github.com/BloomyInDev/my-usage/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
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
