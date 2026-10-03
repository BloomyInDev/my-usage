package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"forge.red.bloomyindev.me/bastien/my-usage/internal/accounts"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/metrics"
	"forge.red.bloomyindev.me/bastien/my-usage/internal/provider"
)

const (
	pollTimeout = 30 * time.Second
	backoffBase = 30 * time.Second
)

// worker polls one account. It is the only owner of that account's
// credentials while it runs.
type worker struct {
	path     string
	acct     accounts.Account
	provider provider.Provider
	interval time.Duration
	store    *metrics.Store
	now      func() time.Time
	log      *slog.Logger
	persist  func(data []byte) error

	// dirty is set while refreshed credentials are only in memory. The old
	// refresh token is spent by then, so saving them comes before any other work.
	dirty bool
}

func (w *worker) run(ctx context.Context) {
	failures := 0
	for {
		err := w.poll(ctx)
		delay := w.interval
		switch {
		case err == nil:
			failures = 0
		case errors.Is(err, provider.ErrPermanent):
			w.log.Error("account disabled until its file changes", "error", err)
			<-ctx.Done()
			return
		default:
			failures++
			delay = backoff(failures, w.interval)
			w.log.Warn("poll failed", "error", err, "retry_in", delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (w *worker) poll(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	start := w.now()
	usage, err := w.fetch(ctx)
	if ctx.Err() != nil && errors.Is(context.Cause(ctx), context.Canceled) {
		return err // shutting down or replaced: leave the last result alone
	}
	w.record(start, usage, err)
	return err
}

func (w *worker) fetch(ctx context.Context) (provider.Usage, error) {
	if err := w.refreshIfNeeded(ctx); err != nil {
		return provider.Usage{}, err
	}
	usage, err := w.provider.FetchUsage(ctx, w.acct.Credentials)
	if err != nil {
		return provider.Usage{}, fmt.Errorf("fetch usage: %w", err)
	}
	if err := usage.Validate(); err != nil {
		return provider.Usage{}, fmt.Errorf("invalid usage: %w", err)
	}
	return usage, nil
}

func (w *worker) refreshIfNeeded(ctx context.Context) error {
	if w.dirty {
		return w.save()
	}
	r, ok := w.provider.(provider.Refresher)
	if !ok || !r.NeedsRefresh(w.acct.Credentials, w.now()) {
		return nil
	}
	fresh, err := r.Refresh(ctx, w.acct.Credentials)
	if err != nil {
		return fmt.Errorf("refresh credentials: %w", err)
	}
	w.acct.Credentials = fresh
	w.dirty = true
	return w.save()
}

func (w *worker) save() error {
	data, err := accounts.Encode(w.acct)
	if err != nil {
		return fmt.Errorf("encode refreshed credentials: %w", err)
	}
	if err := w.persist(data); err != nil {
		return fmt.Errorf("persist refreshed credentials: %w", err)
	}
	w.dirty = false
	return nil
}

func (w *worker) record(start time.Time, usage provider.Usage, err error) {
	end := w.now()
	prev, _ := w.store.Get(w.path)
	r := metrics.Result{
		Provider:    w.acct.Provider,
		Account:     w.acct.Name,
		At:          end,
		LastSuccess: prev.LastSuccess,
		Duration:    end.Sub(start),
		OK:          err == nil,
		Usage:       usage,
	}
	if r.OK {
		r.LastSuccess = end
	}
	w.store.Set(w.path, r)
}

// backoff doubles from backoffBase per consecutive failure, never past max.
func backoff(failures int, max time.Duration) time.Duration {
	d := min(backoffBase, max)
	for i := 1; i < failures && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}
