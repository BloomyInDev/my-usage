// Package codex reads the ChatGPT/Codex subscription quota. It owns its own
// OAuth session, separate from the Codex CLI's, because refresh tokens are
// revoked when two sessions reuse one.
package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/BloomyInDev/my-usage/internal/httpjson"
	"github.com/BloomyInDev/my-usage/internal/provider"
)

const (
	// Both come from the public Codex CLI; they are not documented by OpenAI.
	clientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	refreshMargin = 10 * time.Minute
	// Cloudflare in front of chatgpt.com can reject non-browser agents.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
)

type Credentials struct {
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token"`
	// AccountID is sent as ChatGPT-Account-Id; some accounts need it.
	AccountID string `yaml:"account_id,omitempty"`
	// ExpiresAt is read from the access token when the file omits it.
	ExpiresAt time.Time `yaml:"expires_at,omitempty"`
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
		usageURL: "https://chatgpt.com/backend-api/wham/usage",
		tokenURL: "https://auth.openai.com/oauth/token",
		client:   httpjson.NewClient(),
		now:      time.Now,
	}
}

func (*Provider) Name() string { return "codex" }

func (*Provider) ParseCredentials(raw []byte) (provider.Credentials, error) {
	var c Credentials
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.AccessToken == "" || c.RefreshToken == "" {
		return nil, errors.New("access_token and refresh_token are required")
	}
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = jwtExpiry(c.AccessToken)
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
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": old.RefreshToken,
		"scope":         "openid profile email",
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
	if resp.RefreshToken != "" { // rotated; keep the old one only if not
		fresh.RefreshToken = resp.RefreshToken
	}
	fresh.ExpiresAt = jwtExpiry(resp.AccessToken)
	if fresh.ExpiresAt.IsZero() && resp.ExpiresIn > 0 {
		fresh.ExpiresAt = p.now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	return fresh, nil
}

func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials) (provider.Usage, error) {
	cr := c.(Credentials)
	h := http.Header{
		"Authorization": {"Bearer " + cr.AccessToken},
		"User-Agent":    {userAgent},
	}
	if cr.AccountID != "" {
		h.Set("ChatGPT-Account-Id", cr.AccountID)
	}
	var resp usageResponse
	if err := httpjson.Do(ctx, p.client, http.MethodGet, p.usageURL, h, nil, &resp); err != nil {
		return provider.Usage{}, err
	}
	return parse(resp)
}

// jwtExpiry returns the exp claim of a JWT without verifying it (the server
// verifies it; this only schedules the refresh), or the zero time.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0).UTC()
}
