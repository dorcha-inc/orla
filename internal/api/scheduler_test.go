package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/wire"
)

// recordingSetter captures the last policy installed on the scheduler.
type recordingSetter struct {
	calls   int
	policy  scheduler.SchedulingPolicy
	timeout time.Duration
}

func (r *recordingSetter) SetPolicy(policy scheduler.SchedulingPolicy, timeout time.Duration) {
	r.calls++
	r.policy = policy
	r.timeout = timeout
}

func newSchedulerTestServer(t *testing.T) (*chi.Mux, *settings.FakePolicyStore, *recordingSetter) {
	t.Helper()
	store := &settings.FakePolicyStore{}
	setter := &recordingSetter{}
	r := chi.NewRouter()
	RegisterSchedulerRoutes(r, SchedulerDeps{Store: store, Scheduler: setter})
	return r, store, setter
}

func TestSchedulerPolicy_GetDefaultsToDisabled(t *testing.T) {
	r, _, _ := newSchedulerTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scheduler/policy", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got wire.SchedulerPolicy
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
	assert.False(t, got.Enabled)
	assert.Empty(t, got.URL)
}

func TestSchedulerPolicy_SetPersistsAndInstalls(t *testing.T) {
	r, store, setter := newSchedulerTestServer(t)

	body, _ := json.Marshal(wire.SchedulerPolicy{URL: "http://sched:8090/next", TimeoutMS: 120})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/policy", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// Persisted.
	cfg, err := store.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "http://sched:8090/next", cfg.URL)
	assert.Equal(t, 120*time.Millisecond, cfg.Timeout)

	// Installed live, with a non-nil policy since a URL was given.
	assert.Equal(t, 1, setter.calls)
	assert.NotNil(t, setter.policy)
	assert.Equal(t, 120*time.Millisecond, setter.timeout)
}

func TestSchedulerPolicy_EmptyURLDisables(t *testing.T) {
	r, store, setter := newSchedulerTestServer(t)
	require.NoError(t, store.Set(context.Background(),
		settings.PolicyConfig{URL: "http://sched/next", Timeout: 50 * time.Millisecond}))

	body, _ := json.Marshal(wire.SchedulerPolicy{URL: ""})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/policy", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Nil(t, setter.policy, "an empty URL installs no policy")
	cfg, _ := store.Get(context.Background())
	assert.False(t, cfg.Enabled())
}

func TestSchedulerPolicy_RejectsBadURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "no scheme", url: "sched:8090/next"},
		{name: "wrong scheme", url: "ftp://sched/next"},
		{name: "no host", url: "http:///next"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, setter := newSchedulerTestServer(t)
			body, _ := json.Marshal(wire.SchedulerPolicy{URL: tt.url})
			req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/policy", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Equal(t, 0, setter.calls, "a rejected policy must not be installed")
		})
	}
}

func TestSchedulerPolicy_RejectsNegativeTimeout(t *testing.T) {
	r, _, _ := newSchedulerTestServer(t)
	body, _ := json.Marshal(wire.SchedulerPolicy{URL: "http://sched/next", TimeoutMS: -5})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/scheduler/policy", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}
