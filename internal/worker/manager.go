// Package worker runs one polling goroutine per account file and keeps that
// set in line with the accounts directory.
package worker

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/BloomyInDev/my-usage/internal/accounts"
	"github.com/BloomyInDev/my-usage/internal/metrics"
	"github.com/BloomyInDev/my-usage/internal/provider"
)

type Config struct {
	Dir            accounts.Dir
	Registry       *provider.Registry
	Store          *metrics.Store
	PollInterval   time.Duration
	RescanInterval time.Duration
	Now            func() time.Time
	Logger         *slog.Logger
}

type Manager struct {
	cfg     Config
	entries map[string]*entry
	// bad lists the files that run no worker. A file that failed to parse is
	// remembered by content, so a rescan logs it once instead of every interval.
	bad map[string]badFile
}

type badFile struct {
	hash     [32]byte
	unparsed bool
}

// entry is a running worker. hash is the content of the file the worker was
// started from, or last wrote itself; mu keeps a reader from seeing the file
// and the hash out of step during the worker's own write.
type entry struct {
	path   string
	key    [2]string // provider, account name
	mu     sync.Mutex
	hash   [32]byte
	cancel context.CancelFunc
	done   chan struct{}
}

func New(cfg Config) *Manager {
	return &Manager{cfg: cfg, entries: map[string]*entry{}, bad: map[string]badFile{}}
}

// Run reconciles at start, on every signal from changed, and every
// RescanInterval, until ctx ends and all workers have stopped. A nil changed
// channel is allowed.
func (m *Manager) Run(ctx context.Context, changed <-chan struct{}) {
	m.reconcile(ctx)
	tick := time.NewTicker(m.cfg.RescanInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			for path := range m.entries {
				m.stop(path)
			}
			return
		case _, ok := <-changed:
			if !ok {
				changed = nil
				continue
			}
			m.reconcile(ctx)
		case <-tick.C:
			m.reconcile(ctx)
		}
	}
}

func (m *Manager) reconcile(ctx context.Context) {
	paths, err := m.cfg.Dir.List()
	if err != nil {
		m.cfg.Logger.Error("list accounts", "error", err)
		return
	}
	present := make(map[string]bool, len(paths))
	claimed := make(map[[2]string]string, len(paths))
	for _, path := range paths {
		present[path] = true
		m.sync(ctx, path, claimed)
	}
	for path := range m.entries {
		if !present[path] {
			m.cfg.Logger.Info("account removed", "file", path)
			m.stop(path)
		}
	}
	for path := range m.bad {
		if !present[path] {
			delete(m.bad, path)
			m.cfg.Store.SetInvalid(path, false)
		}
	}
}

func (m *Manager) sync(ctx context.Context, path string, claimed map[[2]string]string) {
	log := m.cfg.Logger.With("file", path)
	e := m.entries[path]

	raw, hash, unchanged, err := m.read(e, path)
	if err != nil {
		log.Error("read account file", "error", err)
		return
	}

	if unchanged {
		if !m.claim(claimed, e.key, path, log) {
			m.stop(path)
			m.markBad(path, badFile{hash: hash})
		}
		return
	}
	if b, ok := m.bad[path]; ok && b.unparsed && b.hash == hash {
		return
	}

	acct, p, err := accounts.Parse(raw, m.cfg.Registry)
	if err != nil {
		log.Error("unusable account file", "error", err)
		m.stop(path)
		m.markBad(path, badFile{hash: hash, unparsed: true})
		return
	}
	key := [2]string{acct.Provider, acct.Name}
	if !m.claim(claimed, key, path, log) {
		m.stop(path)
		m.markBad(path, badFile{hash: hash})
		return
	}
	m.stop(path)
	delete(m.bad, path)
	m.cfg.Store.SetInvalid(path, false)

	wctx, cancel := context.WithCancel(ctx)
	e = &entry{path: path, key: key, hash: hash, cancel: cancel, done: make(chan struct{})}
	m.entries[path] = e
	w := &worker{
		path:     path,
		acct:     acct,
		provider: p,
		interval: m.cfg.PollInterval,
		store:    m.cfg.Store,
		now:      m.cfg.Now,
		log:      m.cfg.Logger.With("file", path, "provider", acct.Provider, "account", acct.Name),
		persist:  e.persist,
	}
	log.Info("account started", "provider", acct.Provider, "account", acct.Name)
	go func() {
		defer close(e.done)
		w.run(wctx)
	}()
}

// read returns the file content and whether it is what the running worker
// already holds. It takes the entry lock, so it cannot see the worker's rename
// before the worker updated its hash.
func (m *Manager) read(e *entry, path string) (raw []byte, hash [32]byte, unchanged bool, err error) {
	if e != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		return nil, hash, false, err
	}
	hash = sha256.Sum256(raw)
	return raw, hash, e != nil && e.hash == hash, nil
}

// claim reserves a provider and account name, which label the series, for one
// file. Two files with the same pair would emit duplicate series and fail the
// whole scrape, so the first file in name order wins.
func (m *Manager) claim(claimed map[[2]string]string, key [2]string, path string, log *slog.Logger) bool {
	if other, taken := claimed[key]; taken {
		log.Error("account name already used by another file", "provider", key[0], "account", key[1], "other_file", other)
		return false
	}
	claimed[key] = path
	return true
}

func (m *Manager) markBad(path string, b badFile) {
	m.bad[path] = b
	m.cfg.Store.SetInvalid(path, true)
}

// stop ends the worker of path, if any, and forgets its result.
func (m *Manager) stop(path string) {
	e, ok := m.entries[path]
	if !ok {
		return
	}
	e.cancel()
	<-e.done
	delete(m.entries, path)
	m.cfg.Store.Delete(path)
}

func (e *entry) persist(data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := accounts.WriteAtomic(e.path, data); err != nil {
		return err
	}
	e.hash = sha256.Sum256(data)
	return nil
}
