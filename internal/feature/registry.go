package feature

import "sort"

// Registry holds the ordered list of features the runner walks through.
// It is intentionally a thin wrapper around a slice – the Groovy version
// relied on Micronaut DI scanning, which we replace with explicit
// construction in main.
type Registry struct {
	features []Feature
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Add registers one or more features. Duplicate names are not detected;
// callers are expected to keep names unique.
func (r *Registry) Add(fs ...Feature) {
	r.features = append(r.features, fs...)
}

// All returns the features sorted by Order ascending. Calling All twice
// returns equivalent slices (no shared backing array).
func (r *Registry) All() []Feature {
	out := make([]Feature, len(r.features))
	copy(out, r.features)
	sort.SliceStable(out, func(i, j int) bool {
		return orderOf(out[i]) < orderOf(out[j])
	})
	return out
}

func orderOf(f Feature) int {
	if o := f.Order(); o != 0 {
		return o
	}
	return 100
}
