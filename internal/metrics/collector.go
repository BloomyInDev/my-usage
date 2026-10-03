package metrics

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

var (
	quotaLabels = []string{"provider", "account", "window", "scope"}
	acctLabels  = []string{"provider", "account"}

	descRatio = prometheus.NewDesc("llm_quota_utilization_ratio",
		"Share of the quota window already used, 0 to 1.", quotaLabels, nil)
	descReset = prometheus.NewDesc("llm_quota_reset_timestamp_seconds",
		"Unix time the quota window resets, when the provider reports it.", quotaLabels, nil)
	descUsed = prometheus.NewDesc("llm_quota_used",
		"Amount of the quota used, when the provider reports it.", quotaLabels, nil)
	descLimit = prometheus.NewDesc("llm_quota_limit",
		"Size of the quota, when the provider reports it.", quotaLabels, nil)
	descInfo = prometheus.NewDesc("llm_provider_info",
		"Plan of the account; always 1.", []string{"provider", "account", "plan"}, nil)
	descSuccess = prometheus.NewDesc("llm_quota_scrape_success",
		"1 if the latest poll succeeded and is still fresh, else 0.", acctLabels, nil)
	descLastOK = prometheus.NewDesc("llm_quota_last_success_timestamp_seconds",
		"Unix time of the latest successful poll.", acctLabels, nil)
	descDuration = prometheus.NewDesc("llm_quota_poll_duration_seconds",
		"Duration of the latest poll.", acctLabels, nil)
	descInvalid = prometheus.NewDesc("llm_quota_invalid_account_files",
		"Account files that cannot be used.", nil, nil)
)

// Collector turns the Store into Prometheus series. A result older than ttl is
// reported as a failure, which covers a worker that is stuck.
type Collector struct {
	store *Store
	ttl   time.Duration
	now   func() time.Time
}

func NewCollector(s *Store, ttl time.Duration, now func() time.Time) *Collector {
	return &Collector{store: s, ttl: ttl, now: now}
}

// Describe sends nothing: the extra metrics are not known up front, which
// makes this an unchecked collector.
func (c *Collector) Describe(chan<- *prometheus.Desc) {}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	results, invalid := c.store.snapshot()
	ch <- prometheus.MustNewConstMetric(descInvalid, prometheus.GaugeValue, float64(invalid))

	now := c.now()
	for _, key := range slices.SortedFunc(maps.Keys(results), cmp.Compare) {
		r := results[key]
		ok := r.OK && now.Sub(r.At) <= c.ttl
		id := []string{r.Provider, r.Account}

		ch <- prometheus.MustNewConstMetric(descSuccess, prometheus.GaugeValue, b2f(ok), id...)
		ch <- prometheus.MustNewConstMetric(descDuration, prometheus.GaugeValue, r.Duration.Seconds(), id...)
		if !r.LastSuccess.IsZero() {
			ch <- prometheus.MustNewConstMetric(descLastOK, prometheus.GaugeValue, unix(r.LastSuccess), id...)
		}
		if !ok {
			continue
		}
		ch <- prometheus.MustNewConstMetric(descInfo, prometheus.GaugeValue, 1, r.Provider, r.Account, r.Usage.Plan)
		for _, q := range r.Usage.Quotas {
			collectQuota(ch, r, q)
		}
		for _, m := range r.Usage.Extras {
			collectExtra(ch, r, m)
		}
	}
}

func collectQuota(ch chan<- prometheus.Metric, r Result, q provider.Quota) {
	l := []string{r.Provider, r.Account, q.Window, q.Scope}
	ch <- prometheus.MustNewConstMetric(descRatio, prometheus.GaugeValue, q.Ratio, l...)
	if q.ResetAt != nil {
		ch <- prometheus.MustNewConstMetric(descReset, prometheus.GaugeValue, unix(*q.ResetAt), l...)
	}
	if q.Used != nil {
		ch <- prometheus.MustNewConstMetric(descUsed, prometheus.GaugeValue, *q.Used, l...)
	}
	if q.Limit != nil {
		ch <- prometheus.MustNewConstMetric(descLimit, prometheus.GaugeValue, *q.Limit, l...)
	}
}

// collectExtra adds provider and account to the labels the provider chose.
func collectExtra(ch chan<- prometheus.Metric, r Result, m provider.Metric) {
	names := []string{"provider", "account"}
	values := []string{r.Provider, r.Account}
	for _, k := range slices.SortedFunc(maps.Keys(m.Labels), cmp.Compare) {
		names = append(names, k)
		values = append(values, m.Labels[k])
	}
	desc := prometheus.NewDesc(m.Name, m.Help, names, nil)
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, m.Value, values...)
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func unix(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }
