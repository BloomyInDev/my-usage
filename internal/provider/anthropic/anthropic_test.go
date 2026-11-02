package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

// usage_pro.json is reconstructed from what is known of /api/oauth/usage, not
// captured from a live account. Replace it with a real response.
func TestParseProFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/usage_pro.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp usageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	u, err := parse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Quotas) != 2 || u.Quotas[0].Window != "5h" || u.Quotas[0].Ratio != 0.37 || u.Quotas[1].Window != "weekly" {
		t.Fatalf("quotas %+v", u.Quotas)
	}
	if u.Quotas[0].ResetAt == nil || u.Quotas[0].ResetAt.Year() != 2026 {
		t.Fatalf("reset %v", u.Quotas[0].ResetAt)
	}
	if err := u.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseUnknownShape(t *testing.T) {
	var resp usageResponse
	json.Unmarshal([]byte(`{"something":{"utilization":5}}`), &resp)
	if _, err := parse(resp); err == nil {
		t.Fatal("no known window must be an error")
	}
	json.Unmarshal([]byte(`{"five_hour":{"resets_at":null}}`), &resp)
	if _, err := parse(resp); err == nil {
		t.Fatal("a window without utilization must be an error")
	}
}

func TestNeedsRefresh(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	p := New()
	for name, tc := range map[string]struct {
		exp  time.Time
		want bool
	}{
		"far":     {now.Add(time.Hour), false},
		"margin":  {now.Add(9 * time.Minute), true},
		"unknown": {time.Time{}, true},
	} {
		if got := p.NeedsRefresh(Credentials{ExpiresAt: tc.exp}, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestRefresh(t *testing.T) {
	now := time.Unix(1_000_000, 0).UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] == "dead" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		if body["refresh_token"] == "no-expiry" {
			w.Write([]byte(`{"access_token":"a3","refresh_token":"r3"}`))
			return
		}
		if body["grant_type"] != "refresh_token" || body["client_id"] != clientID {
			t.Errorf("request %v", body)
		}
		w.Write([]byte(`{"access_token":"a2","refresh_token":"r2","expires_in":3600}`))
	}))
	defer srv.Close()
	p := &Provider{tokenURL: srv.URL, client: srv.Client(), now: func() time.Time { return now }}

	got, err := p.Refresh(context.Background(), Credentials{AccessToken: "a", RefreshToken: "r1", SubscriptionType: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	c := got.(Credentials)
	if c.AccessToken != "a2" || c.RefreshToken != "r2" || c.SubscriptionType != "pro" || !c.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("credentials %+v", c)
	}

	got, err = p.Refresh(context.Background(), Credentials{RefreshToken: "no-expiry", ExpiresAt: now})
	if err != nil {
		t.Fatalf("a missing expires_in must keep the new tokens, got %v", err)
	}
	if c := got.(Credentials); c.AccessToken != "a3" || c.RefreshToken != "r3" || !c.ExpiresAt.IsZero() {
		t.Fatalf("credentials %+v", c)
	}

	_, err = p.Refresh(context.Background(), Credentials{RefreshToken: "dead"})
	if !errors.Is(err, provider.ErrPermanent) {
		t.Fatalf("a rejected refresh token is permanent, got %v", err)
	}
}

func TestFetchUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("Anthropic-Beta") != betaHeader {
			t.Errorf("headers %v", r.Header)
		}
		w.Write([]byte(`{"five_hour":{"utilization":50,"resets_at":null}}`))
	}))
	defer srv.Close()
	p := &Provider{usageURL: srv.URL, client: srv.Client(), now: time.Now}
	u, err := p.FetchUsage(context.Background(), Credentials{AccessToken: "tok", SubscriptionType: "max"})
	if err != nil || u.Plan != "max" || u.Quotas[0].Ratio != 0.5 || u.Quotas[0].ResetAt != nil {
		t.Fatalf("%+v, %v", u, err)
	}
}
