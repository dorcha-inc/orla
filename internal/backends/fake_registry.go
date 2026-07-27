package backends

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"
)

// FakeRegistry is an in-memory Registry for tests. Safe for concurrent
// use.
type FakeRegistry struct {
	mu       sync.RWMutex
	backends map[string]*Backend
	now      func() time.Time
}

// NewFakeRegistry returns an empty in-memory registry.
func NewFakeRegistry() *FakeRegistry {
	return &FakeRegistry{
		backends: make(map[string]*Backend),
		now:      time.Now,
	}
}

// Compile-time interface check.
var _ Registry = (*FakeRegistry)(nil)

// SetNow overrides the clock for deterministic tests.
func (r *FakeRegistry) SetNow(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

func (r *FakeRegistry) Insert(_ context.Context, b *Backend) (*Backend, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.backends[b.Name]; ok {
		return nil, ErrAlreadyExists
	}
	now := r.now()
	stored := cloneBackend(b)
	stored.CreatedAt = now
	stored.UpdatedAt = now
	if stored.MaxConcurrency == 0 {
		stored.MaxConcurrency = 1
	}
	r.backends[b.Name] = stored
	return cloneBackend(stored), nil
}

func (r *FakeRegistry) Get(_ context.Context, name string) (*Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneBackend(b), nil
}

func (r *FakeRegistry) List(_ context.Context) ([]*Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := slices.Sorted(maps.Keys(r.backends))
	out := make([]*Backend, 0, len(names))
	for _, n := range names {
		out = append(out, cloneBackend(r.backends[n]))
	}
	return out, nil
}

func (r *FakeRegistry) Patch(_ context.Context, name string, p PatchRequest) (*Backend, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.backends[name]
	if !ok {
		return nil, ErrNotFound
	}
	updated := *current
	if p.Endpoint != nil {
		updated.Endpoint = *p.Endpoint
	}
	if p.APIKeyEnvVar != nil {
		updated.APIKeyEnvVar = *p.APIKeyEnvVar
	}
	if p.MaxConcurrency != nil {
		updated.MaxConcurrency = *p.MaxConcurrency
	}
	if p.InputCostPerMtoken != nil {
		v := *p.InputCostPerMtoken
		updated.InputCostPerMtoken = &v
	}
	if p.OutputCostPerMtoken != nil {
		v := *p.OutputCostPerMtoken
		updated.OutputCostPerMtoken = &v
	}
	if p.Quality != nil {
		v := *p.Quality
		updated.Quality = &v
	}
	if p.RatePerSecond != nil {
		v := *p.RatePerSecond
		updated.RatePerSecond = &v
	}
	if p.Rates != nil {
		updated.Rates = maps.Clone(*p.Rates)
	}
	if p.CostSource != nil {
		if *p.CostSource == "" {
			updated.CostSource = nil
		} else {
			v := *p.CostSource
			updated.CostSource = &v
		}
	}
	updated.UpdatedAt = r.now()
	r.backends[name] = &updated
	return cloneBackend(&updated), nil
}

func (r *FakeRegistry) Delete(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.backends[name]; !ok {
		return ErrNotFound
	}
	delete(r.backends, name)
	return nil
}

func cloneBackend(b *Backend) *Backend {
	out := *b
	if b.InputCostPerMtoken != nil {
		v := *b.InputCostPerMtoken
		out.InputCostPerMtoken = &v
	}
	if b.OutputCostPerMtoken != nil {
		v := *b.OutputCostPerMtoken
		out.OutputCostPerMtoken = &v
	}
	if b.Quality != nil {
		v := *b.Quality
		out.Quality = &v
	}
	if b.RatePerSecond != nil {
		v := *b.RatePerSecond
		out.RatePerSecond = &v
	}
	if b.CostSource != nil {
		v := *b.CostSource
		out.CostSource = &v
	}
	out.Rates = maps.Clone(b.Rates)
	return &out
}
