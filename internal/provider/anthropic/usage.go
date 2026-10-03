package anthropic

import (
	"errors"
	"fmt"
	"time"

	"github.com/BloomyInDev/my-usage/internal/provider"
)

type usageResponse struct {
	FiveHour     *window `json:"five_hour"`
	SevenDay     *window `json:"seven_day"`
	SevenDayOpus *window `json:"seven_day_opus"`
	SevenDaySon  *window `json:"seven_day_sonnet"`
}

type window struct {
	Utilization *float64   `json:"utilization"` // percent, 0 to 100
	ResetsAt    *time.Time `json:"resets_at"`
}

func parse(r usageResponse) (provider.Usage, error) {
	var u provider.Usage
	for _, w := range []struct {
		w            *window
		label, scope string
	}{
		{r.FiveHour, "5h", ""},
		{r.SevenDay, "weekly", ""},
		{r.SevenDayOpus, "weekly", "opus"},
		{r.SevenDaySon, "weekly", "sonnet"},
	} {
		if w.w == nil {
			continue
		}
		if w.w.Utilization == nil {
			return u, fmt.Errorf("%s window lacks utilization", w.label)
		}
		u.Quotas = append(u.Quotas, provider.Quota{
			Window:  w.label,
			Scope:   w.scope,
			Ratio:   *w.w.Utilization / 100,
			ResetAt: w.w.ResetsAt,
		})
	}
	if len(u.Quotas) == 0 {
		return u, errors.New("no known quota window in the response")
	}
	return u, nil
}
