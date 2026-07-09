package scheduler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/scheduler"
)

// fakePolicy decides admission with an injectable function so a test
// can order, error, time out, or return an unknown id.
type fakePolicy struct {
	next func(ctx context.Context, backend scheduler.BackendView, pending []scheduler.PendingRequest) (string, error)
}

func (f *fakePolicy) Next(ctx context.Context, backend scheduler.BackendView, pending []scheduler.PendingRequest) (string, error) {
	return f.next(ctx, backend, pending)
}

// pickStage returns a policy that admits the queued request whose stage
// matches want, falling back to the oldest if none match.
func pickStage(want string) *fakePolicy {
	return &fakePolicy{next: func(_ context.Context, _ scheduler.BackendView, pending []scheduler.PendingRequest) (string, error) {
		for _, p := range pending {
			if p.Info.Stage == want {
				return p.ID, nil
			}
		}
		return pending[0].ID, nil
	}}
}

type fakePolicyMetrics struct {
	mu       sync.Mutex
	outcomes map[string]int
}

func newFakePolicyMetrics() *fakePolicyMetrics {
	return &fakePolicyMetrics{outcomes: map[string]int{}}
}

func (m *fakePolicyMetrics) IncPolicyDecision(_ /*backend*/, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes[outcome]++
}

func (m *fakePolicyMetrics) ObservePolicyDecision(string, float64) {}

func (m *fakePolicyMetrics) count(outcome string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.outcomes[outcome]
}

// acq drives one Acquire in its own goroutine so the test can queue
// several and observe which one the scheduler admits.
type acq struct {
	stage    string
	admitted chan scheduler.ReleaseFunc
	failed   chan error
}

func startAcquire(s *scheduler.Scheduler, backend, stage string) *acq {
	a := &acq{
		stage:    stage,
		admitted: make(chan scheduler.ReleaseFunc, 1),
		failed:   make(chan error, 1),
	}
	go func() {
		_, release, err := s.AcquireLLM(context.Background(), backend, scheduler.RequestInfo{Stage: stage})
		if err != nil {
			a.failed <- err
			return
		}
		a.admitted <- release
	}()
	return a
}

func (a *acq) waitAdmitted(t *testing.T) scheduler.ReleaseFunc {
	t.Helper()
	select {
	case release := <-a.admitted:
		return release
	case err := <-a.failed:
		t.Fatalf("acquire for stage %q failed: %v", a.stage, err)
	case <-time.After(2 * time.Second):
		t.Fatalf("acquire for stage %q was not admitted", a.stage)
	}
	return nil
}

func (a *acq) assertWaiting(t *testing.T) {
	t.Helper()
	select {
	case <-a.admitted:
		t.Fatalf("stage %q was admitted but should still be queued", a.stage)
	case err := <-a.failed:
		t.Fatalf("stage %q failed but should still be queued: %v", a.stage, err)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitQueueDepth(t *testing.T, s *scheduler.Scheduler, backend string, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, st := range s.Stats() {
			if st.Backend == backend {
				return st.QueueDepth == want
			}
		}
		return false
	}, 2*time.Second, time.Millisecond, "queue depth never reached %d", want)
}

// newSingleSlotScheduler registers one backend with capacity 1 and
// returns the scheduler plus a held ReleaseFunc for the only slot, so
// every later Acquire queues until the caller releases.
func newSingleSlotScheduler(t *testing.T, opts ...scheduler.Option) (*scheduler.Scheduler, scheduler.ReleaseFunc) {
	t.Helper()
	s := scheduler.New(mockFactory(nil), quietLogger(), opts...)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	s.Register(newBackend("b", 1))

	held := startAcquire(s, "b", "holder")
	release := held.waitAdmitted(t)
	return s, release
}

func TestExecutor_PolicyChoosesAdmissionOrder(t *testing.T) {
	s, release := newSingleSlotScheduler(t, scheduler.WithPolicy(pickStage("urgent")))

	first := startAcquire(s, "b", "a")
	waitQueueDepth(t, s, "b", 1)
	urgent := startAcquire(s, "b", "urgent")
	waitQueueDepth(t, s, "b", 2)
	last := startAcquire(s, "b", "b")
	waitQueueDepth(t, s, "b", 3)

	// Freeing the only slot triggers exactly one admission.
	release()

	rel := urgent.waitAdmitted(t)
	first.assertWaiting(t)
	last.assertWaiting(t)

	// Drain the rest so the goroutines exit before shutdown.
	rel()
	first.waitAdmitted(t)()
	last.waitAdmitted(t)()
}

func TestExecutor_FallsBackToFCFSOnPolicyError(t *testing.T) {
	metrics := newFakePolicyMetrics()
	errPolicy := &fakePolicy{next: func(context.Context, scheduler.BackendView, []scheduler.PendingRequest) (string, error) {
		return "", errors.New("policy exploded")
	}}
	s, release := newSingleSlotScheduler(t,
		scheduler.WithPolicy(errPolicy),
		scheduler.WithPolicyMetrics(metrics),
	)

	oldest := startAcquire(s, "b", "oldest")
	waitQueueDepth(t, s, "b", 1)
	newer := startAcquire(s, "b", "newer")
	waitQueueDepth(t, s, "b", 2)

	// The holder's own admission already went through the policy, so
	// measure the increment that freeing the slot causes.
	before := metrics.count("fallback_error")
	release()

	// Exactly one admission follows: the oldest waiter. newer must stay
	// queued until oldest releases its slot, so assert that before
	// releasing so a freed slot cannot admit newer under us.
	relOldest := oldest.waitAdmitted(t)
	assert.Equal(t, before+1, metrics.count("fallback_error"))
	newer.assertWaiting(t)

	relOldest()
	newer.waitAdmitted(t)()
}

func TestExecutor_FallsBackToFCFSOnPolicyTimeout(t *testing.T) {
	metrics := newFakePolicyMetrics()
	slowPolicy := &fakePolicy{next: func(ctx context.Context, _ scheduler.BackendView, _ []scheduler.PendingRequest) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	s, release := newSingleSlotScheduler(t,
		scheduler.WithPolicy(slowPolicy),
		scheduler.WithPolicyMetrics(metrics),
		scheduler.WithDecisionTimeout(20*time.Millisecond),
	)

	oldest := startAcquire(s, "b", "oldest")
	waitQueueDepth(t, s, "b", 1)
	newer := startAcquire(s, "b", "newer")
	waitQueueDepth(t, s, "b", 2)

	before := metrics.count("fallback_timeout")
	release()

	relOldest := oldest.waitAdmitted(t)
	assert.Equal(t, before+1, metrics.count("fallback_timeout"))
	newer.assertWaiting(t)

	relOldest()
	newer.waitAdmitted(t)()
}

func TestExecutor_FallsBackToFCFSOnUnknownID(t *testing.T) {
	metrics := newFakePolicyMetrics()
	ghostPolicy := &fakePolicy{next: func(context.Context, scheduler.BackendView, []scheduler.PendingRequest) (string, error) {
		return "no-such-request", nil
	}}
	s, release := newSingleSlotScheduler(t,
		scheduler.WithPolicy(ghostPolicy),
		scheduler.WithPolicyMetrics(metrics),
	)

	oldest := startAcquire(s, "b", "oldest")
	waitQueueDepth(t, s, "b", 1)

	before := metrics.count("fallback_invalid")
	release()

	relOldest := oldest.waitAdmitted(t)
	assert.Equal(t, before+1, metrics.count("fallback_invalid"))
	relOldest()
}

func TestExecutor_CanceledWaiterLeavesQueueWithoutLeak(t *testing.T) {
	s := scheduler.New(mockFactory(nil), quietLogger())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	s.Register(newBackend("b", 1))

	holder := startAcquire(s, "b", "holder")
	release := holder.waitAdmitted(t)

	// A waiter that cancels while queued must vacate the queue and must
	// not consume the slot it never received.
	ctx, cancel := context.WithCancel(context.Background())
	gaveUp := make(chan error, 1)
	go func() {
		_, _, err := s.AcquireLLM(ctx, "b", scheduler.RequestInfo{Stage: "doomed"})
		gaveUp <- err
	}()
	waitQueueDepth(t, s, "b", 1)
	cancel()
	require.Error(t, <-gaveUp)
	waitQueueDepth(t, s, "b", 0)

	// The slot is still held by holder. Releasing it must let a fresh
	// request through, proving the canceled waiter did not leak a slot.
	release()
	next := startAcquire(s, "b", "next")
	next.waitAdmitted(t)()
}

func TestScheduler_SetPolicyTakesEffectLive(t *testing.T) {
	// Start with no policy (fcfs), then install one live and confirm the
	// next admission uses it.
	s, release := newSingleSlotScheduler(t)
	s.SetPolicy(pickStage("urgent"), 50*time.Millisecond)

	first := startAcquire(s, "b", "a")
	waitQueueDepth(t, s, "b", 1)
	urgent := startAcquire(s, "b", "urgent")
	waitQueueDepth(t, s, "b", 2)

	release()

	rel := urgent.waitAdmitted(t)
	first.assertWaiting(t)

	rel()
	first.waitAdmitted(t)()
}

func TestHTTPPolicy_Decisions(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantID  string
		wantErr bool
	}{
		{name: "next field", status: 200, body: `{"next":"id2"}`, wantID: "id2"},
		{name: "order fallback", status: 200, body: `{"order":["id3","id1"]}`, wantID: "id3"},
		{name: "empty decision", status: 200, body: `{}`, wantErr: true},
		{name: "non-200", status: 503, body: `nope`, wantErr: true},
		{name: "malformed json", status: 200, body: `{`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var decoded map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&decoded))
				assert.Contains(t, decoded, "backend")
				assert.Contains(t, decoded, "pending")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			policy := scheduler.NewHTTPPolicy(srv.URL, time.Second)
			id, err := policy.Next(context.Background(),
				scheduler.BackendView{Name: "b", Capacity: 1},
				[]scheduler.PendingRequest{
					{ID: "id1", Info: scheduler.RequestInfo{Stage: "s"}, EnqueuedAt: time.Now()},
					{ID: "id2", EnqueuedAt: time.Now()},
					{ID: "id3", EnqueuedAt: time.Now()},
				})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestHTTPPolicy_TransportErrorSurfaces(t *testing.T) {
	// Nothing is listening, so Do fails at the transport layer.
	policy := scheduler.NewHTTPPolicy("http://127.0.0.1:1/schedule", 200*time.Millisecond)
	_, err := policy.Next(context.Background(),
		scheduler.BackendView{Name: "b"},
		[]scheduler.PendingRequest{{ID: "id1", EnqueuedAt: time.Now()}})
	assert.Error(t, err)
}
