// Package costs polls backend cost sources and holds the live prices.
//
// A backend may carry a cost_source URL. The Poller fetches every
// configured source on a fixed interval and stores the result in a
// Store. The proxy consults the Store when it computes the cost of a
// completion and falls back to the backend's static columns when no
// live price is held. A fetch failure keeps the last known price, so
// a flapping cost service degrades to slightly stale prices rather
// than missing cost records.
package costs

import (
	"maps"
	"sync"
)

// Price is one backend's live per-million-token costs.
type Price struct {
	InputPerMtoken  float64
	OutputPerMtoken float64
}

// Store holds the live price for each backend with a cost source.
// The zero value is not usable, construct with NewStore. Safe for
// concurrent use.
type Store struct {
	mu     sync.RWMutex
	prices map[string]Price
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{prices: make(map[string]Price)}
}

// Get returns the live price for the named backend and whether one is
// held.
func (s *Store) Get(name string) (Price, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prices[name]
	return p, ok
}

// Set records the live price for the named backend.
func (s *Store) Set(name string, p Price) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prices[name] = p
}

// Retain drops every entry whose backend is not in keep, so a backend
// whose cost source was removed returns to its static costs.
func (s *Store) Retain(keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.prices, func(name string, _ Price) bool {
		return !keep[name]
	})
}
