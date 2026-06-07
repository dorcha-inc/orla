package scheduler

import (
	"fmt"
	"sync"
	"time"
)

type cbState int

const (
	cbClosed  cbState = iota
	cbOpen
	cbHalfOpen
)

// CircuitOpenError is returned when a request is rejected because
// the circuit is open. The error is created to help distinguish
// an open circuit from backend-related errors.
type CircuitOpenError struct{ Backend string }

func (e *CircuitOpenError) Error() string {
	return fmt.Sprintf("circuit open for backend %q: service unavailable", e.Backend)
}

type circuitBreaker struct {
	mu               sync.Mutex
	state            cbState
	consecutiveFails int
	openedAt         time.Time
	threshold        int
	openTimeout      time.Duration
	probeInFlight    bool
}

func newCircuitBreaker(threshold int, openTimeout time.Duration) *circuitBreaker {
	return &circuitBreaker{
		threshold:   threshold,
		openTimeout: openTimeout,
	}
}

// allow reports whether the circuit breaker will let a new request through.
//
// Closed: always returns true.
// Open: returns false until openTimeout has elapsed, then transitions to
// HalfOpen and returns true for the first caller (the probe request).
// HalfOpen: returns true only if no probe is already in flight. probeInFlight
// prevents a recovering backend from being flooded when the timeout expires,
// since only one request should test whether the backend has recovered.
func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.openedAt) < cb.openTimeout {
			return false
		}
		cb.state = cbHalfOpen
		cb.probeInFlight = true
		return true
	case cbHalfOpen:
		if cb.probeInFlight {
			return false
		}
		cb.probeInFlight = true
		return true
	}
	return true
}

// recordSuccess resets the failure counter, clears the probe flag, and
// closes the circuit.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFails = 0
	cb.probeInFlight = false
	cb.state = cbClosed
}

// recordFailure advances failure state. 
// Only call this for backend errors (5xx, 429, connection errors). 
// Client errors (4xx) and context cancellations must not call this method. 
// At threshold consecutive failures the circuit opens. 
// A failure in HalfOpen reopens the circuit immediately.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		cb.consecutiveFails++
		if cb.consecutiveFails >= cb.threshold {
			cb.state = cbOpen
			cb.openedAt = time.Now()
		}
	case cbHalfOpen:
		cb.state = cbOpen
		cb.openedAt = time.Now()
		cb.probeInFlight = false
	}
}
