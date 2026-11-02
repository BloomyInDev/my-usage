// Package provider defines what the core needs from a subscription provider
// and the normalized shape every provider reports its quota in.
package provider

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// ErrPermanent marks a failure retrying cannot fix (revoked token, rejected
// key). The core stops polling the account until its file changes.
var ErrPermanent = errors.New("permanent provider failure")

// Provider fetches the quota of one account. Credentials are the YAML mapping
// found under the `credentials` key of the account file.
type Provider interface {
	Name() string
	ParseCredentials(raw []byte) (Credentials, error)
	FetchUsage(ctx context.Context, c Credentials) (Usage, error)
}

// Refresher is implemented only by providers whose session this tool owns.
// Refresh returns the new credentials; the core persists them before anything
// else, because the old refresh token is spent once the call succeeds.
type Refresher interface {
	NeedsRefresh(c Credentials, now time.Time) bool
	Refresh(ctx context.Context, c Credentials) (Credentials, error)
}

// Credentials is opaque to the core. Marshal returns the YAML mapping to store
// back under the `credentials` key.
type Credentials interface {
	Marshal() ([]byte, error)
}

// Quota is one rate-limit window. Optional values are nil when the provider
// does not report them.
type Quota struct {
	Window  string // "5h", "weekly", "monthly", "session"
	Scope   string // optional: "all", "chatpass"
	Ratio   float64
	ResetAt *time.Time
	Used    *float64
	Limit   *float64
}

// Metric is a provider-specific gauge that is not a ratio (per-model request
// counts, credits, model availability). Name must start with "llm_".
type Metric struct {
	Name   string
	Help   string
	Labels map[string]string
	Value  float64
}

type Usage struct {
	Plan   string
	Quotas []Quota
	Extras []Metric
}

// Validate rejects a Usage the exporter must not publish. An account without
// a single recognized window is a failed scrape, never 0 %.
func (u Usage) Validate() error {
	if len(u.Quotas) == 0 {
		return errors.New("no quota window in the response")
	}
	seen := make(map[[2]string]bool, len(u.Quotas))
	for _, q := range u.Quotas {
		if q.Window == "" {
			return errors.New("quota with an empty window")
		}
		// No upper bound: a provider may report usage past the limit, and
		// that is the moment the value matters most.
		if math.IsNaN(q.Ratio) || q.Ratio < 0 {
			return fmt.Errorf("quota %q has invalid ratio %v", q.Window, q.Ratio)
		}
		k := [2]string{q.Window, q.Scope}
		if seen[k] {
			return fmt.Errorf("duplicate quota window %q scope %q", q.Window, q.Scope)
		}
		seen[k] = true
	}
	extras := make(map[string]bool, len(u.Extras))
	for _, m := range u.Extras {
		if !strings.HasPrefix(m.Name, "llm_") {
			return fmt.Errorf("extra metric %q lacks the llm_ prefix", m.Name)
		}
		if math.IsNaN(m.Value) {
			return fmt.Errorf("extra metric %q is NaN", m.Name)
		}
		k := m.Name + "\x00" + labelsKey(m.Labels)
		if extras[k] {
			return fmt.Errorf("duplicate extra metric %q with the same labels", m.Name)
		}
		extras[k] = true
	}
	return nil
}

func labelsKey(l map[string]string) string {
	var b strings.Builder
	for _, k := range sortedKeys(l) {
		b.WriteString(k + "=" + l[k] + "\x00")
	}
	return b.String()
}
