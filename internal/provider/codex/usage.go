package codex

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

// The response also carries user_id, account_id and email. They are not
// decoded on purpose, so they cannot reach a label or a log.
type usageResponse struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		Primary   *window `json:"primary_window"`
		Secondary *window `json:"secondary_window"`
	} `json:"rate_limit"`
	Chatpass *struct {
		Windows []*window `json:"windows"`
	} `json:"chatpass"`
	Credits *struct {
		Balance *number `json:"balance"`
	} `json:"credits"`
}

type window struct {
	UsedPercent        *float64 `json:"used_percent"` // 0 to 100
	LimitWindowSeconds *int64   `json:"limit_window_seconds"`
	ResetAt            *int64   `json:"reset_at"` // Unix seconds
}

// number reads a JSON number or a numeric string.
type number float64

func (n *number) UnmarshalJSON(b []byte) error {
	s := string(bytes.Trim(b, `"`))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("not a number: %s", b)
	}
	*n = number(f)
	return nil
}

func parse(r usageResponse) (provider.Usage, error) {
	u := provider.Usage{Plan: r.PlanType}
	add := func(w *window, scope string) error {
		if w == nil {
			return nil
		}
		q, err := w.quota(scope)
		if err != nil {
			return err
		}
		u.Quotas = append(u.Quotas, q)
		return nil
	}
	if r.RateLimit != nil {
		if err := add(r.RateLimit.Primary, ""); err != nil {
			return u, err
		}
		if err := add(r.RateLimit.Secondary, ""); err != nil {
			return u, err
		}
	}
	if r.Chatpass != nil {
		for _, w := range r.Chatpass.Windows {
			if err := add(w, "chatpass"); err != nil {
				return u, err
			}
		}
	}
	if r.Credits != nil && r.Credits.Balance != nil {
		u.Extras = append(u.Extras, provider.Metric{
			Name:  "llm_credits_balance",
			Help:  "Credit balance of the account.",
			Value: float64(*r.Credits.Balance),
		})
	}
	return u, nil
}

// quota fails on a window that is present but incomplete: guessing its length
// or its usage would publish a made-up number.
func (w *window) quota(scope string) (provider.Quota, error) {
	if w.UsedPercent == nil || w.LimitWindowSeconds == nil {
		return provider.Quota{}, errors.New("quota window lacks used_percent or limit_window_seconds")
	}
	q := provider.Quota{
		Window: windowLabel(*w.LimitWindowSeconds),
		Scope:  scope,
		Ratio:  *w.UsedPercent / 100,
	}
	// While usage is zero the window has not started and reset_at keeps
	// sliding; it is still published as reported.
	if w.ResetAt != nil {
		t := time.Unix(*w.ResetAt, 0).UTC()
		q.ResetAt = &t
	}
	return q, nil
}

// windowLabel names a window after its length, since OpenAI changes the
// windows per plan.
func windowLabel(seconds int64) string {
	const hour, day = 3600, 86400
	switch seconds {
	case 5 * hour:
		return "5h"
	case 7 * day:
		return "weekly"
	case 30 * day:
		return "monthly"
	}
	switch {
	case seconds%day == 0:
		return strconv.FormatInt(seconds/day, 10) + "d"
	case seconds%hour == 0:
		return strconv.FormatInt(seconds/hour, 10) + "h"
	}
	return strconv.FormatInt(seconds, 10) + "s"
}
