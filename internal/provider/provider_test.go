package provider

import "testing"

func TestUsageValidate(t *testing.T) {
	q := func(w, s string, r float64) Quota { return Quota{Window: w, Scope: s, Ratio: r} }
	tests := []struct {
		name    string
		usage   Usage
		wantErr bool
	}{
		{"one window", Usage{Quotas: []Quota{q("5h", "", 0.4)}}, false},
		{"bounds", Usage{Quotas: []Quota{q("5h", "", 0), q("weekly", "", 1)}}, false},
		{"no window", Usage{}, true},
		{"ratio above one", Usage{Quotas: []Quota{q("5h", "", 1.01)}}, true},
		{"negative ratio", Usage{Quotas: []Quota{q("5h", "", -0.1)}}, true},
		{"empty window", Usage{Quotas: []Quota{q("", "", 0.1)}}, true},
		{"same window, other scope", Usage{Quotas: []Quota{q("5h", "a", 0.1), q("5h", "b", 0.1)}}, false},
		{"duplicate window", Usage{Quotas: []Quota{q("5h", "", 0.1), q("5h", "", 0.2)}}, true},
		{"extra without prefix", Usage{Quotas: []Quota{q("5h", "", 0.1)}, Extras: []Metric{{Name: "foo"}}}, true},
		{"duplicate extra", Usage{Quotas: []Quota{q("5h", "", 0.1)}, Extras: []Metric{
			{Name: "llm_x", Labels: map[string]string{"model": "a"}},
			{Name: "llm_x", Labels: map[string]string{"model": "a"}},
		}}, true},
		{"same extra, other label", Usage{Quotas: []Quota{q("5h", "", 0.1)}, Extras: []Metric{
			{Name: "llm_x", Labels: map[string]string{"model": "a"}},
			{Name: "llm_x", Labels: map[string]string{"model": "b"}},
		}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.usage.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
