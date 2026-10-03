// Package metrics holds the latest poll result of every account and exposes
// them to Prometheus. Series are built at scrape time from those results, so a
// failed or expired account emits no quota value instead of a stale one.
package metrics

import (
	"sync"
	"time"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

// Result is the outcome of the latest poll of one account.
type Result struct {
	Provider string
	Account  string
	// At is when the poll finished. LastSuccess is zero until one succeeded.
	At          time.Time
	LastSuccess time.Time
	Duration    time.Duration
	// OK is true only when Usage is valid and fresh.
	OK    bool
	Usage provider.Usage
}

// Store is safe for concurrent use. Keys are account file paths.
type Store struct {
	mu      sync.RWMutex
	results map[string]Result
	invalid map[string]bool
}

func NewStore() *Store {
	return &Store{results: map[string]Result{}, invalid: map[string]bool{}}
}

func (s *Store) Set(key string, r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[key] = r
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.results, key)
}

// Get returns the stored result, e.g. to carry LastSuccess across a failure.
func (s *Store) Get(key string) (Result, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.results[key]
	return r, ok
}

// SetInvalid records whether the account file at key is unusable (unreadable,
// malformed, unknown provider, duplicate name).
func (s *Store) SetInvalid(key string, invalid bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if invalid {
		s.invalid[key] = true
	} else {
		delete(s.invalid, key)
	}
}

func (s *Store) snapshot() (map[string]Result, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Result, len(s.results))
	for k, v := range s.results {
		out[k] = v
	}
	return out, len(s.invalid)
}
