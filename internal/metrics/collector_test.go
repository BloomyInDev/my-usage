package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

var t0 = time.Unix(1_000_000, 0)

func collect(t *testing.T, s *Store, now time.Time, want string, names ...string) {
	t.Helper()
	c := NewCollector(s, 10*time.Minute, func() time.Time { return now })
	if err := testutil.CollectAndCompare(c, strings.NewReader(want), names...); err != nil {
		t.Fatal(err)
	}
}

func okResult() Result {
	reset := time.Unix(1_000_600, 0)
	used := 42.0
	return Result{
		Provider: "ollama", Account: "main", At: t0, LastSuccess: t0, OK: true,
		Usage: provider.Usage{
			Plan: "pro",
			Quotas: []provider.Quota{
				{Window: "5h", Ratio: 0.25, ResetAt: &reset, Used: &used},
				{Window: "weekly", Scope: "all", Ratio: 0.5},
			},
			Extras: []provider.Metric{{Name: "llm_model_requests", Help: "h",
				Labels: map[string]string{"model": "m1"}, Value: 7}},
		},
	}
}

func TestCollectOptionalValuesOnlyWhenPresent(t *testing.T) {
	s := NewStore()
	s.Set("a.yaml", okResult())

	collect(t, s, t0, `
# HELP llm_quota_utilization_ratio Share of the quota window already used, 0 to 1.
# TYPE llm_quota_utilization_ratio gauge
llm_quota_utilization_ratio{account="main",provider="ollama",scope="",window="5h"} 0.25
llm_quota_utilization_ratio{account="main",provider="ollama",scope="all",window="weekly"} 0.5
`, "llm_quota_utilization_ratio")

	collect(t, s, t0, `
# HELP llm_quota_reset_timestamp_seconds Unix time the quota window resets, when the provider reports it.
# TYPE llm_quota_reset_timestamp_seconds gauge
llm_quota_reset_timestamp_seconds{account="main",provider="ollama",scope="",window="5h"} 1.0006e+06
`, "llm_quota_reset_timestamp_seconds")

	collect(t, s, t0, `
# HELP llm_model_requests h
# TYPE llm_model_requests gauge
llm_model_requests{account="main",model="m1",provider="ollama"} 7
`, "llm_model_requests")
}

func TestCollectFailureEmitsNoQuota(t *testing.T) {
	failed := okResult()
	failed.OK = false
	expired := okResult()

	for name, tc := range map[string]struct {
		r   Result
		now time.Time
	}{
		"failed":  {failed, t0},
		"expired": {expired, t0.Add(11 * time.Minute)},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewStore()
			s.Set("a.yaml", tc.r)
			collect(t, s, tc.now, "", "llm_quota_utilization_ratio", "llm_provider_info", "llm_model_requests")
			collect(t, s, tc.now, `
# HELP llm_quota_scrape_success 1 if the latest poll succeeded and is still fresh, else 0.
# TYPE llm_quota_scrape_success gauge
llm_quota_scrape_success{account="main",provider="ollama"} 0
`, "llm_quota_scrape_success")
		})
	}
}

func TestCollectInvalidAndDelete(t *testing.T) {
	s := NewStore()
	s.SetInvalid("bad.yaml", true)
	s.Set("a.yaml", okResult())
	s.Delete("a.yaml")
	collect(t, s, t0, `
# HELP llm_quota_invalid_account_files Account files that cannot be used.
# TYPE llm_quota_invalid_account_files gauge
llm_quota_invalid_account_files 1
`, "llm_quota_invalid_account_files", "llm_quota_scrape_success")
}
