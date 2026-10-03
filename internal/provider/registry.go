package provider

import (
	"cmp"
	"maps"
	"slices"
)

// Registry maps the `provider` field of an account file to its implementation.
type Registry struct {
	byName map[string]Provider
}

func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byName: make(map[string]Provider, len(ps))}
	for _, p := range ps {
		if _, dup := r.byName[p.Name()]; dup {
			panic("provider: duplicate registration of " + p.Name())
		}
		r.byName[p.Name()] = p
	}
	return r
}

func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.SortedFunc(maps.Keys(m), cmp.Compare)
}
