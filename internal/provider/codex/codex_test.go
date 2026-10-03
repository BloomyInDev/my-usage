package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

func jwt(exp int64) string {
	b := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return b(`{"alg":"none"}`) + "." + b(`{"exp":`+strconv.FormatInt(exp, 10)+`}`) + ".sig"
}

// usage_free.json is reconstructed from the free-plan response described in
// BEGIN.md section 6.3, not captured live. Replace it with a real capture.
func TestParseFreePlan(t *testing.T) {
	raw, err := os.ReadFile("testdata/usage_free.json")
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
	if u.Plan != "free" || len(u.Quotas) != 2 {
		t.Fatalf("usage %+v", u)
	}
	m := u.Quotas[0]
	if m.Window != "monthly" || m.Scope != "" || m.Ratio != 0 || m.ResetAt == nil || m.ResetAt.Unix() != 1790000000 {
		t.Fatalf("primary %+v", m)
	}
	c := u.Quotas[1]
	if c.Window != "5h" || c.Scope != "chatpass" || c.Ratio != 0.125 {
		t.Fatalf("chatpass %+v", c)
	}
	if len(u.Extras) != 0 {
		t.Fatalf("null balance must emit nothing, got %+v", u.Extras)
	}
	if err := u.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseCreditsAndIncompleteWindow(t *testing.T) {
	var resp usageResponse
	json.Unmarshal([]byte(`{"credits":{"balance":"4.5"},"rate_limit":{"primary_window":{"used_percent":50,"limit_window_seconds":604800}}}`), &resp)
	u, err := parse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Extras) != 1 || u.Extras[0].Value != 4.5 || u.Quotas[0].Window != "weekly" || u.Quotas[0].ResetAt != nil {
		t.Fatalf("usage %+v", u)
	}

	json.Unmarshal([]byte(`{"rate_limit":{"primary_window":{"used_percent":50}}}`), &resp)
	resp.RateLimit.Primary.LimitWindowSeconds = nil
	if _, err := parse(resp); err == nil {
		t.Fatal("a window without a length must fail")
	}
}

func TestWindowLabel(t *testing.T) {
	for sec, want := range map[int64]string{18000: "5h", 604800: "weekly", 2592000: "monthly", 86400: "1d", 7200: "2h", 90: "90s"} {
		if got := windowLabel(sec); got != want {
			t.Errorf("windowLabel(%d) = %q, want %q", sec, got, want)
		}
	}
}

func TestParseCredentialsReadsExpiryFromToken(t *testing.T) {
	exp := time.Now().Add(time.Hour).Unix()
	c, err := New().ParseCredentials([]byte("access_token: " + jwt(exp) + "\nrefresh_token: r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.(Credentials).ExpiresAt.Unix(); got != exp {
		t.Fatalf("expiry %d, want %d", got, exp)
	}
	if _, err := New().ParseCredentials([]byte("access_token: a\n")); err == nil {
		t.Fatal("missing refresh_token must fail")
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
		"expired": {now.Add(-time.Minute), true},
		"unknown": {time.Time{}, true},
	} {
		if got := p.NeedsRefresh(Credentials{ExpiresAt: tc.exp}, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestRefresh(t *testing.T) {
	newExp := time.Now().Add(time.Hour).Unix()
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		if gotBody["refresh_token"] == "dead" {
			http.Error(w, `{"error":"refresh_token_reused"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": jwt(newExp), "refresh_token": "r2"})
	}))
	defer srv.Close()
	p := &Provider{tokenURL: srv.URL, client: srv.Client(), now: time.Now}

	got, err := p.Refresh(context.Background(), Credentials{AccessToken: "a", RefreshToken: "r1", AccountID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	c := got.(Credentials)
	if c.RefreshToken != "r2" || c.AccountID != "id" || c.ExpiresAt.Unix() != newExp {
		t.Fatalf("credentials %+v", c)
	}
	if gotBody["grant_type"] != "refresh_token" || gotBody["client_id"] != clientID {
		t.Fatalf("request %v", gotBody)
	}

	_, err = p.Refresh(context.Background(), Credentials{RefreshToken: "dead"})
	if !errors.Is(err, provider.ErrPermanent) {
		t.Fatalf("a rejected refresh token is permanent, got %v", err)
	}
}

func TestFetchUsageHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("ChatGPT-Account-Id") != "acc" || r.Header.Get("User-Agent") == "" {
			t.Errorf("headers %v", r.Header)
		}
		w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000}}}`))
	}))
	defer srv.Close()
	p := &Provider{usageURL: srv.URL, client: srv.Client(), now: time.Now}
	u, err := p.FetchUsage(context.Background(), Credentials{AccessToken: "tok", AccountID: "acc"})
	if err != nil || u.Plan != "plus" || u.Quotas[0].Ratio != 0.1 {
		t.Fatalf("%+v, %v", u, err)
	}
}
