package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/BloomyInDev/my-usage/internal/accounts"
	"github.com/BloomyInDev/my-usage/internal/metrics"
	"github.com/BloomyInDev/my-usage/internal/provider"
)

type creds struct {
	Token string `yaml:"token"`
}

func (c creds) Marshal() ([]byte, error) { return yaml.Marshal(c) }

// fake is a Provider and Refresher whose behaviour each test sets.
type fake struct {
	parses  atomic.Int32
	fetches atomic.Int32

	mu          sync.Mutex
	fetch       func(c creds) (provider.Usage, error)
	needRefresh func(c creds) bool
	refresh     func(c creds) (creds, error)
}

func (f *fake) Name() string { return "fake" }
func (f *fake) ParseCredentials(raw []byte) (provider.Credentials, error) {
	f.parses.Add(1)
	var c creds
	return c, yaml.Unmarshal(raw, &c)
}
func (f *fake) FetchUsage(_ context.Context, c provider.Credentials) (provider.Usage, error) {
	f.fetches.Add(1)
	f.mu.Lock()
	fn := f.fetch
	f.mu.Unlock()
	return fn(c.(creds))
}
func (f *fake) NeedsRefresh(c provider.Credentials, _ time.Time) bool {
	f.mu.Lock()
	fn := f.needRefresh
	f.mu.Unlock()
	return fn != nil && fn(c.(creds))
}
func (f *fake) Refresh(_ context.Context, c provider.Credentials) (provider.Credentials, error) {
	return f.refresh(c.(creds))
}

func goodUsage(creds) (provider.Usage, error) {
	return provider.Usage{Plan: "p", Quotas: []provider.Quota{{Window: "5h", Ratio: 0.1}}}, nil
}

type env struct {
	dir   string
	store *metrics.Store
	fake  *fake
}

func start(t *testing.T, f *fake) *env {
	t.Helper()
	e := &env{dir: t.TempDir(), store: metrics.NewStore(), fake: f}
	if f.fetch == nil {
		f.fetch = goodUsage
	}
	m := New(Config{
		Dir:            accounts.Dir(e.dir),
		Registry:       provider.NewRegistry(f),
		Store:          e.store,
		PollInterval:   time.Hour,
		RescanInterval: 10 * time.Millisecond,
		Now:            time.Now,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx, nil) }()
	t.Cleanup(func() { cancel(); <-done })
	return e
}

func (e *env) write(t *testing.T, file, name, token string) string {
	t.Helper()
	path := filepath.Join(e.dir, file)
	body := "provider: fake\nname: " + name + "\ncredentials:\n  token: " + token + "\n"
	if err := accounts.WriteAtomic(path, []byte(body)); err != nil {
		t.Fatal(err)
	}
	return path
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *env) result(path string) (metrics.Result, bool) { return e.store.Get(path) }

func TestAccountAddedAndRemoved(t *testing.T) {
	e := start(t, &fake{})
	path := e.write(t, "a.yaml", "main", "tok")

	eventually(t, "first result", func() bool { r, ok := e.result(path); return ok && r.OK })
	if r, _ := e.result(path); r.Account != "main" || r.Provider != "fake" {
		t.Fatalf("unexpected result %+v", r)
	}

	os.Remove(path)
	eventually(t, "result removed", func() bool { _, ok := e.result(path); return !ok })
}

func TestEditedAccountRestartsWorker(t *testing.T) {
	e := start(t, &fake{})
	path := e.write(t, "a.yaml", "main", "one")
	eventually(t, "first poll", func() bool { r, ok := e.result(path); return ok && r.OK })

	e.write(t, "a.yaml", "renamed", "two")
	eventually(t, "new name", func() bool { r, _ := e.result(path); return r.Account == "renamed" && r.OK })
}

func TestEmptyUsageIsAFailure(t *testing.T) {
	e := start(t, &fake{fetch: func(creds) (provider.Usage, error) { return provider.Usage{Plan: "p"}, nil }})
	path := e.write(t, "a.yaml", "main", "tok")

	eventually(t, "a result", func() bool { _, ok := e.result(path); return ok })
	if r, _ := e.result(path); r.OK {
		t.Fatal("a response without any window must not be a success")
	}
}

func TestPermanentErrorStopsPolling(t *testing.T) {
	f := &fake{fetch: func(creds) (provider.Usage, error) {
		return provider.Usage{}, errors.Join(provider.ErrPermanent, errors.New("revoked"))
	}}
	e := start(t, f)
	path := e.write(t, "a.yaml", "main", "tok")

	eventually(t, "a failed result", func() bool { r, ok := e.result(path); return ok && !r.OK })
	time.Sleep(100 * time.Millisecond) // several rescans
	if n := f.fetches.Load(); n != 1 {
		t.Fatalf("fetched %d times, want 1", n)
	}
}

// The refreshed token must be on disk before the usage call, and the worker's
// own write must not make the manager restart it.
func TestRefreshPersistedBeforeFetchWithoutRestart(t *testing.T) {
	var path string
	var onDisk atomic.Value
	f := &fake{
		needRefresh: func(c creds) bool { return c.Token == "old" },
		refresh:     func(creds) (creds, error) { return creds{Token: "new"}, nil },
	}
	f.fetch = func(c creds) (provider.Usage, error) {
		raw, _ := os.ReadFile(path)
		onDisk.Store(string(raw))
		return goodUsage(c)
	}
	e := start(t, f)
	path = e.write(t, "a.yaml", "main", "old")

	eventually(t, "first poll", func() bool { r, ok := e.result(path); return ok && r.OK })
	want := "provider: fake\nname: main\ncredentials:\n  token: new\n"
	if got := onDisk.Load(); got != want {
		t.Fatalf("file during fetch:\n%v\nwant:\n%s", got, want)
	}
	time.Sleep(100 * time.Millisecond) // several rescans
	if n := f.parses.Load(); n != 1 {
		t.Fatalf("account parsed %d times: the worker's own write restarted it", n)
	}
}

func TestRefreshFailureIsReported(t *testing.T) {
	f := &fake{
		needRefresh: func(creds) bool { return true },
		refresh:     func(creds) (creds, error) { return creds{}, errors.New("boom") },
	}
	e := start(t, f)
	path := e.write(t, "a.yaml", "main", "old")

	eventually(t, "a failed result", func() bool { r, ok := e.result(path); return ok && !r.OK })
	if n := f.fetches.Load(); n != 0 {
		t.Fatalf("fetched %d times after a failed refresh, want 0", n)
	}
}

func TestInvalidAndDuplicateFiles(t *testing.T) {
	e := start(t, &fake{})
	a := e.write(t, "a.yaml", "main", "tok")
	b := e.write(t, "b.yaml", "main", "tok") // same provider and name as a
	bad := filepath.Join(e.dir, "c.yaml")
	os.WriteFile(bad, []byte("provider: ["), 0o600)

	eventually(t, "a running", func() bool { r, ok := e.result(a); return ok && r.OK })
	time.Sleep(100 * time.Millisecond)
	if _, ok := e.result(b); ok {
		t.Fatal("duplicate account must not run")
	}

	// Removing the winner lets the other file take the name over.
	os.Remove(a)
	eventually(t, "b takes over", func() bool { r, ok := e.result(b); return ok && r.OK })
}

func TestBackoff(t *testing.T) {
	for _, tc := range []struct {
		failures int
		max      time.Duration
		want     time.Duration
	}{
		{1, 5 * time.Minute, 30 * time.Second},
		{2, 5 * time.Minute, time.Minute},
		{4, 5 * time.Minute, 4 * time.Minute},
		{5, 5 * time.Minute, 5 * time.Minute},
		{1, 10 * time.Second, 10 * time.Second},
	} {
		if got := backoff(tc.failures, tc.max); got != tc.want {
			t.Errorf("backoff(%d, %v) = %v, want %v", tc.failures, tc.max, got, tc.want)
		}
	}
}
