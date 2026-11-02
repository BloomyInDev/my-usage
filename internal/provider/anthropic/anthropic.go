// Package anthropic reads the Claude subscription quota. It owns its own
// OAuth session, separate from the Claude Code CLI's, because refresh tokens
// are revoked when two sessions reuse one.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/BloomyInDev/my-usage/internal/httpjson"
	"github.com/BloomyInDev/my-usage/internal/provider"
)

const (
	// Both come from the public Claude Code CLI; Anthropic does not document them.
	clientID      = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	betaHeader    = "oauth-2025-04-20"
	refreshMargin = 10 * time.Minute
)

type Credentials struct {
	AccessToken  string    `yaml:"access_token"`
	RefreshToken string    `yaml:"refresh_token"`
	ExpiresAt    time.Time `yaml:"expires_at"`
	// SubscriptionType ("pro", "max") is shown as the plan; optional.
	SubscriptionType string `yaml:"subscription_type,omitempty"`
}

func (c Credentials) Marshal() ([]byte, error) { return yaml.Marshal(c) }

type Provider struct {
	usageURL string
	tokenURL string
	client   *http.Client
	now      func() time.Time
}

func New() *Provider {
	return &Provider{
		usageURL: "https://api.anthropic.com/api/oauth/usage",
		tokenURL: "https://console.anthropic.com/v1/oauth/token",
		client:   httpjson.NewClient(),
		now:      time.Now,
	}
}

func (*Provider) Name() string { return "anthropic" }

func (*Provider) ParseCredentials(raw []byte) (provider.Credentials, error) {
	var c Credentials
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.AccessToken == "" || c.RefreshToken == "" {
		return nil, errors.New("access_token and refresh_token are required")
	}
	return c, nil
}

// NeedsRefresh is true when the expiry is unknown too: refreshing is safe
// since this tool owns the session.
func (*Provider) NeedsRefresh(c provider.Credentials, now time.Time) bool {
	exp := c.(Credentials).ExpiresAt
	return exp.IsZero() || now.Add(refreshMargin).After(exp)
}

func (p *Provider) Refresh(ctx context.Context, c provider.Credentials) (provider.Credentials, error) {
	old := c.(Credentials)
	req := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": old.RefreshToken,
		"client_id":     clientID,
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := httpjson.Do(ctx, p.client, http.MethodPost, p.tokenURL, nil, req, &resp); err != nil {
		var se *httpjson.StatusError
		if errors.As(err, &se) && (se.Code == http.StatusBadRequest || se.Code == http.StatusUnauthorized) {
			return nil, fmt.Errorf("%w: %w", provider.ErrPermanent, err)
		}
		return nil, err
	}
	if resp.AccessToken == "" {
		return nil, errors.New("refresh response has no access_token")
	}
	fresh := old
	fresh.AccessToken = resp.AccessToken
	if resp.RefreshToken != "" {
		fresh.RefreshToken = resp.RefreshToken
	}
	// The old refresh token is spent by now, so the new pair must be kept even
	// without an expiry. A zero expiry refreshes again on the next poll.
	fresh.ExpiresAt = time.Time{}
	if resp.ExpiresIn > 0 {
		fresh.ExpiresAt = p.now().Add(time.Duration(resp.ExpiresIn) * time.Second).UTC()
	}
	return fresh, nil
}

func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials) (provider.Usage, error) {
	cr := c.(Credentials)
	h := http.Header{
		"Authorization":  {"Bearer " + cr.AccessToken},
		"Anthropic-Beta": {betaHeader},
	}
	var resp usageResponse
	if err := httpjson.Do(ctx, p.client, http.MethodGet, p.usageURL, h, nil, &resp); err != nil {
		return provider.Usage{}, err
	}
	u, err := parse(resp)
	u.Plan = cr.SubscriptionType
	return u, err
}
