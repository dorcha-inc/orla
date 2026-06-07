package scheduler

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultCB() *circuitBreaker {
	return newCircuitBreaker(5, 60*time.Second)
}

// rewindOpen moves openedAt into the past so the timeout appears elapsed.
// Used to avoid sleeping 60s in tests.
func rewindOpen(cb *circuitBreaker, by time.Duration) {
	cb.mu.Lock()
	cb.openedAt = time.Now().Add(-by)
	cb.mu.Unlock()
}

func TestCircuitBreaker_InitiallyClosed(t *testing.T) {
	cb := defaultCB()
	assert.True(t, cb.allow())
	assert.Equal(t, cbClosed, cb.state)
}

func TestCircuitBreaker_OpensAtThreshold(t *testing.T) {
	cb := newCircuitBreaker(3, 60*time.Second)
	cb.recordFailure()
	cb.recordFailure()
	assert.Equal(t, cbClosed, cb.state, "below threshold: still closed")
	cb.recordFailure()
	assert.Equal(t, cbOpen, cb.state, "at threshold: circuit opens")
}

func TestCircuitBreaker_BlocksRequestsWhenOpen(t *testing.T) {
	cb := newCircuitBreaker(1, 60*time.Second)
	cb.recordFailure()
	require.Equal(t, cbOpen, cb.state)
	assert.False(t, cb.allow())
	assert.False(t, cb.allow(), "repeated allow calls must all block while open")
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	cb := newCircuitBreaker(1, 60*time.Second)
	cb.recordFailure()
	require.Equal(t, cbOpen, cb.state)

	rewindOpen(cb, 61*time.Second)

	allowed := cb.allow()
	assert.True(t, allowed, "should allow probe after timeout elapses")
	assert.Equal(t, cbHalfOpen, cb.state)
	assert.True(t, cb.probeInFlight)
}

func TestCircuitBreaker_SuccessfulProbeCloses(t *testing.T) {
	cb := newCircuitBreaker(1, 60*time.Second)
	cb.recordFailure()
	rewindOpen(cb, 61*time.Second)

	require.True(t, cb.allow()) // transitions to HalfOpen
	cb.recordSuccess()

	assert.Equal(t, cbClosed, cb.state)
	assert.False(t, cb.probeInFlight)
	assert.True(t, cb.allow(), "circuit should accept requests after successful probe")
}

func TestCircuitBreaker_FailedProbeReopens(t *testing.T) {
	cb := newCircuitBreaker(1, 60*time.Second)
	cb.recordFailure()
	rewindOpen(cb, 61*time.Second)

	require.True(t, cb.allow()) // transitions to HalfOpen
	cb.recordFailure()

	assert.Equal(t, cbOpen, cb.state)
	assert.False(t, cb.probeInFlight)
	assert.False(t, cb.allow(), "circuit should block again after failed probe")
}

func TestCircuitBreaker_SuccessResetsFailureCounter(t *testing.T) {
	cb := newCircuitBreaker(5, 60*time.Second)
	for range 4 {
		cb.recordFailure()
	}
	assert.Equal(t, cbClosed, cb.state)

	cb.recordSuccess()
	assert.Equal(t, 0, cb.consecutiveFails, "success must reset consecutive failure count")

	// Four more failures should still be below threshold.
	for range 4 {
		cb.recordFailure()
	}
	assert.Equal(t, cbClosed, cb.state, "circuit must stay closed after counter reset")
}

func TestCircuitBreaker_ConcurrentHalfOpen_OnlyOneProbe(t *testing.T) {
	cb := newCircuitBreaker(1, 60*time.Second)
	cb.recordFailure()
	rewindOpen(cb, 61*time.Second)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	allowed := make([]bool, goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			allowed[i] = cb.allow()
		}(i)
	}
	wg.Wait()

	probes := 0
	for _, a := range allowed {
		if a {
			probes++
		}
	}
	assert.Equal(t, 1, probes, "exactly one probe must pass in half-open state")
}

func TestCircuitBreaker_CircuitOpenError_Message(t *testing.T) {
	err := &CircuitOpenError{Backend: "gpu-a"}
	assert.Equal(t, `circuit open for backend "gpu-a": service unavailable`, err.Error())
}
