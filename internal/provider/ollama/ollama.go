// Package ollama reads the subscription quota of an Ollama Cloud account.
// The endpoints are private and undocumented, so every shape assumption here
// fails loudly instead of defaulting.
package ollama

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.yaml.in/yaml/v3"

	"github.com/BloomyInDev/my-usage/internal/httpjson"
	"github.com/BloomyInDev/my-usage/internal/provider"
)

type Credentials struct {
	APIKey string `yaml:"api_key"`
}

func (c Credentials) Marshal() ([]byte, error) { return yaml.Marshal(c) }

type Provider struct {
	baseURL string
	client  *http.Client
}

func New() *Provider {
	return &Provider{baseURL: "https://ollama.com", client: httpjson.NewClient()}
}

func (*Provider) Name() string { return "ollama" }

func (*Provider) ParseCredentials(raw []byte) (provider.Credentials, error) {
	var c Credentials
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.APIKey == "" {
		return nil, errors.New("api_key is empty")
	}
	return c, nil
}

// windows lists the limits Ollama has reported, in the order they are emitted.
// Plans differ: older ones have session and weekly, newer ones only monthly.
var windows = []struct{ key, label string }{
	{"session", "5h"},
	{"weekly", "weekly"},
	{"monthly", "monthly"},
}

type usageResponse struct {
	Limits map[string]*struct {
		Usage *float64 `json:"usage"` // fraction, 0 to 1
	} `json:"limits"`
}

func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials) (provider.Usage, error) {
	h := http.Header{"Authorization": {"Bearer " + c.(Credentials).APIKey}}

	var resp usageResponse
	if err := httpjson.Do(ctx, p.client, http.MethodGet, p.baseURL+"/api/usage", h, nil, &resp); err != nil {
		return provider.Usage{}, classify(err)
	}
	usage := parse(resp)
	usage.Plan = p.plan(ctx, h)
	return usage, nil
}

func parse(resp usageResponse) provider.Usage {
	var u provider.Usage
	for _, w := range windows {
		if l := resp.Limits[w.key]; l != nil && l.Usage != nil {
			// No reset time: the API does not return one.
			u.Quotas = append(u.Quotas, provider.Quota{Window: w.label, Ratio: *l.Usage})
		}
	}
	return u
}

// plan is informational, so a failure here must not fail the scrape.
func (p *Provider) plan(ctx context.Context, h http.Header) string {
	var me struct {
		Plan string `json:"plan"`
	}
	if err := httpjson.Do(ctx, p.client, http.MethodPost, p.baseURL+"/api/me", h, nil, &me); err != nil {
		return ""
	}
	return me.Plan
}

// classify makes a rejected static key permanent: nothing but a new key in the
// account file can fix it.
func classify(err error) error {
	var se *httpjson.StatusError
	if errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden) {
		return fmt.Errorf("%w: %w", provider.ErrPermanent, err)
	}
	return err
}
