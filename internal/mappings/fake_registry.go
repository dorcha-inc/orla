package mappings

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"
)

// FakeRegistry is an in-memory Registry for tests. It is safe for
// concurrent use, mutations take a write lock.
type FakeRegistry struct {
	mu       sync.RWMutex
	variants map[string]*Variant
	now      func() time.Time
}

// NewFakeRegistry returns an empty in-memory registry.
func NewFakeRegistry() *FakeRegistry {
	return &FakeRegistry{
		variants: make(map[string]*Variant),
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

func (r *FakeRegistry) Resolve(_ context.Context, variant, stageID string) (string, bool) {
	if variant == "" {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.variants[variant]
	if !ok {
		return "", false
	}
	backend, ok := v.Overrides[stageID]
	return backend, ok
}

func (r *FakeRegistry) Put(_ context.Context, name string, overrides map[string]string) (*Variant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	created := now
	if existing, ok := r.variants[name]; ok {
		created = existing.CreatedAt
	}
	stored := &Variant{
		Name:      name,
		Overrides: maps.Clone(overrides),
		CreatedAt: created,
		UpdatedAt: now,
	}
	if stored.Overrides == nil {
		stored.Overrides = map[string]string{}
	}
	r.variants[name] = stored
	return cloneVariant(stored), nil
}

func (r *FakeRegistry) Get(_ context.Context, name string) (*Variant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.variants[name]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneVariant(v), nil
}

func (r *FakeRegistry) List(_ context.Context) ([]*Variant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := slices.Sorted(maps.Keys(r.variants))
	out := make([]*Variant, 0, len(names))
	for _, name := range names {
		out = append(out, cloneVariant(r.variants[name]))
	}
	return out, nil
}

func (r *FakeRegistry) Delete(_ context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.variants[name]; !ok {
		return ErrNotFound
	}
	delete(r.variants, name)
	return nil
}

func cloneVariant(v *Variant) *Variant {
	out := *v
	out.Overrides = maps.Clone(v.Overrides)
	if out.Overrides == nil {
		out.Overrides = map[string]string{}
	}
	return &out
}
