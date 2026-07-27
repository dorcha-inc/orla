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
	"slices"
	"sync"
	"time"
)

// Price is one backend's live per-million-token costs.
type Price struct {
	InputPerMtoken  float64
	OutputPerMtoken float64
}

// Stat reports one backend's held price and how long ago it was
// fetched. A price that keeps aging means the cost source is failing
// and completions are being priced from stale data.
type Stat struct {
	Backend string
	Price   Price
	Age     time.Duration
}

type entry struct {
	price     Price
	fetchedAt time.Time
}

// Store holds the live price for each backend with a cost source.
// The zero value is not usable, construct with NewStore. Safe for
// concurrent use.
type Store struct {
	mu      sync.RWMutex
	now     func() time.Time
	entries map[string]entry
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{now: time.Now, entries: make(map[string]entry)}
}

// Get returns the live price for the named backend and whether one is
// held.
func (s *Store) Get(name string) (Price, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[name]
	return e.price, ok
}

// Set records the live price for the named backend and stamps it with
// the current time.
func (s *Store) Set(name string, p Price) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[name] = entry{price: p, fetchedAt: s.now()}
}

// Retain drops every entry whose backend is not in keep, so a backend
// whose cost source was removed returns to its static costs.
func (s *Store) Retain(keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.entries, func(name string, _ entry) bool {
		return !keep[name]
	})
}

// Stats returns one Stat per held price, ordered by backend name.
func (s *Store) Stats() []Stat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	out := make([]Stat, 0, len(s.entries))
	for _, name := range slices.Sorted(maps.Keys(s.entries)) {
		e := s.entries[name]
		out = append(out, Stat{
			Backend: name,
			Price:   e.price,
			Age:     now.Sub(e.fetchedAt),
		})
	}
	return out
}
