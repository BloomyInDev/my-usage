package ollama

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

// The fixtures are reconstructed from the field names in BEGIN.md section 6.2,
// not captured from a live account. Replace them with real responses.
func serve(t *testing.T, status int, file string) *Provider {
	t.Helper()
	body, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/usage":
			w.WriteHeader(status)
			w.Write(body)
		case "/api/me":
			w.Write([]byte(`{"plan":"pro"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return &Provider{baseURL: srv.URL, client: srv.Client()}
}

func fetch(p *Provider) (provider.Usage, error) {
	return p.FetchUsage(context.Background(), Credentials{APIKey: "key"})
}

func TestMonthlyOnlyPlan(t *testing.T) {
	u, err := fetch(serve(t, 200, "usage_monthly.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Quotas) != 1 || u.Quotas[0].Window != "monthly" || u.Quotas[0].Ratio != 0.37 {
		t.Fatalf("quotas %+v", u.Quotas)
	}
	if u.Quotas[0].ResetAt != nil {
		t.Fatal("Ollama gives no reset time; none must be invented")
	}
	if u.Plan != "pro" {
		t.Fatalf("plan %q", u.Plan)
	}
	if err := u.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionWeeklyPlan(t *testing.T) {
	u, err := fetch(serve(t, 200, "usage_session_weekly.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Quotas) != 2 || u.Quotas[0].Window != "5h" || u.Quotas[1].Window != "weekly" {
		t.Fatalf("quotas %+v", u.Quotas)
	}
}

func TestUnknownShapeYieldsNoQuota(t *testing.T) {
	p := serve(t, 200, "usage_monthly.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"something":"else"}`))
	}))
	defer srv.Close()
	p.baseURL = srv.URL

	u, err := fetch(p)
	if err != nil {
		t.Fatal(err)
	}
	if u.Validate() == nil {
		t.Fatal("a response with no known window must not validate")
	}
}

func TestRejectedKeyIsPermanent(t *testing.T) {
	_, err := fetch(serve(t, http.StatusUnauthorized, "usage_monthly.json"))
	if !errors.Is(err, provider.ErrPermanent) {
		t.Fatalf("want ErrPermanent, got %v", err)
	}
	_, err = fetch(serve(t, http.StatusInternalServerError, "usage_monthly.json"))
	if err == nil || errors.Is(err, provider.ErrPermanent) {
		t.Fatalf("a 500 is transient, got %v", err)
	}
}

func TestParseCredentials(t *testing.T) {
	p := New()
	if _, err := p.ParseCredentials([]byte("api_key: abc\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ParseCredentials([]byte("api_key: \"\"\n")); err == nil {
		t.Fatal("empty key must fail")
	}
}
