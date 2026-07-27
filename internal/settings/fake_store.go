package settings

import (
	"context"
	"sync"
)

// FakePolicyStore is an in-memory PolicyStore for tests. The zero value
// is ready to use and returns the first-come-first-served default until
// Set is called.
type FakePolicyStore struct {
	mu  sync.Mutex
	cfg PolicyConfig
}

// Compile-time interface check.
var _ PolicyStore = (*FakePolicyStore)(nil)

func (s *FakePolicyStore) Get(_ context.Context) (PolicyConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg, nil
}

func (s *FakePolicyStore) Set(_ context.Context, cfg PolicyConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	return nil
}

// FakeCostStore is an in-memory CostStore for tests. The zero value is
// ready to use and returns DefaultCostRefreshInterval until Set is
// called.
type FakeCostStore struct {
	mu  sync.Mutex
	cfg CostConfig
}

// Compile-time interface check.
var _ CostStore = (*FakeCostStore)(nil)

func (s *FakeCostStore) Get(_ context.Context) (CostConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CostConfig{RefreshInterval: s.cfg.Interval()}, nil
}

func (s *FakeCostStore) Set(_ context.Context, cfg CostConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	return nil
}
