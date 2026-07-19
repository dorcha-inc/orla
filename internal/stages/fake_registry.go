package stages

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"
)

// FakeRegistry is an in-memory Registry intended for tests. It is safe
// for concurrent use, mutations take a write lock.
type FakeRegistry struct {
	mu     sync.RWMutex
	stages map[string]*Stage
	now    func() time.Time
}

// NewFakeRegistry returns an empty in-memory registry.
func NewFakeRegistry() *FakeRegistry {
	return &FakeRegistry{
		stages: make(map[string]*Stage),
		now:    time.Now,
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

func (r *FakeRegistry) GetOrCreate(_ context.Context, id string) (*Stage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.stages[id]; ok {
		return cloneStage(existing), nil
	}
	now := r.now()
	s := &Stage{
		ID:        id,
		Labels:    map[string]any{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.stages[id] = s
	return cloneStage(s), nil
}

func (r *FakeRegistry) Get(_ context.Context, id string) (*Stage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stages[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneStage(s), nil
}

func (r *FakeRegistry) List(_ context.Context) ([]*Stage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := slices.Sorted(maps.Keys(r.stages))
	out := make([]*Stage, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneStage(r.stages[id]))
	}
	return out, nil
}

func (r *FakeRegistry) Replace(_ context.Context, s *Stage) (*Stage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	created := now
	if existing, ok := r.stages[s.ID]; ok {
		created = existing.CreatedAt
	}
	labels := s.Labels
	if labels == nil {
		labels = map[string]any{}
	}
	stored := &Stage{
		ID:              s.ID,
		Backend:         s.Backend,
		ReasoningEffort: s.ReasoningEffort,
		Prompt:          s.Prompt,
		CaptureIO:       s.CaptureIO,
		Labels:          cloneLabels(labels),
		CreatedAt:       created,
		UpdatedAt:       now,
	}
	r.stages[s.ID] = stored
	return cloneStage(stored), nil
}

func (r *FakeRegistry) Patch(_ context.Context, id string, p PatchRequest) (*Stage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.stages[id]
	if !ok {
		return nil, ErrNotFound
	}
	updated := *current
	if p.Backend != nil {
		updated.Backend = *p.Backend
	}
	if p.ReasoningEffort != nil {
		updated.ReasoningEffort = *p.ReasoningEffort
	}
	if p.Prompt != nil {
		updated.Prompt = *p.Prompt
	}
	if p.CaptureIO != nil {
		updated.CaptureIO = *p.CaptureIO
	}
	if p.Labels != nil {
		updated.Labels = cloneLabels(p.Labels)
	}
	updated.UpdatedAt = r.now()
	r.stages[id] = &updated
	return cloneStage(&updated), nil
}

func (r *FakeRegistry) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.stages[id]; !ok {
		return ErrNotFound
	}
	delete(r.stages, id)
	return nil
}

func cloneStage(s *Stage) *Stage {
	out := *s
	out.Labels = cloneLabels(s.Labels)
	return &out
}

func cloneLabels(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}
