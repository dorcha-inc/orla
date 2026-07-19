package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeControlPlaneAuditMetrics is the hand-written ControlPlaneAuditMetrics
// recorder shared by this file's tests.
type fakeControlPlaneAuditMetrics struct {
	mu  sync.Mutex
	got []string // "resource|method|outcome"
}

func (f *fakeControlPlaneAuditMetrics) IncControlPlaneMutation(resource, method, outcome string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, resource+"|"+method+"|"+outcome)
}

func (f *fakeControlPlaneAuditMetrics) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func TestControlPlaneResource_Outcomes(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		want   string
		wantOK bool
	}{
		{name: "resource with id", path: "/api/v1/stages/foo", want: "stages", wantOK: true},
		{name: "list, no id", path: "/api/v1/stages", want: "stages", wantOK: true},
		{name: "list, trailing slash", path: "/api/v1/stages/", want: "stages", wantOK: true},
		{name: "backends resource", path: "/api/v1/backends/gpt4o", want: "backends", wantOK: true},
		{name: "mappings resource", path: "/api/v1/mappings/cand", want: "mappings", wantOK: true},
		{name: "nested scheduler policy path", path: "/api/v1/scheduler/policy", want: "scheduler", wantOK: true},
		{name: "not a control-plane path", path: "/v1/chat/completions", want: "", wantOK: false},
		{name: "prefix only, no resource", path: "/api/v1/", want: "", wantOK: false},
		{name: "root", path: "/", want: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := controlPlaneResource(tt.path)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestAuditControlPlaneMutations_SkipsReads(t *testing.T) {
	metrics := &fakeControlPlaneAuditMetrics{}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	mw := AuditControlPlaneMutations(logger, metrics)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequest(method, "/api/v1/stages/foo", nil)
		rr := httptest.NewRecorder()
		mw(next).ServeHTTP(rr, req)
		assert.True(t, called, "handler must still run for %s", method)
	}

	assert.Empty(t, metrics.snapshot())
	assert.Empty(t, buf.String())
}

func TestAuditControlPlaneMutations_RecordsMutations(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		wantResource string
	}{
		{name: "post mappings", method: http.MethodPost, path: "/api/v1/mappings", wantResource: "mappings"},
		{name: "put stage", method: http.MethodPut, path: "/api/v1/stages/foo", wantResource: "stages"},
		{name: "patch backend", method: http.MethodPatch, path: "/api/v1/backends/gpt4o", wantResource: "backends"},
		{name: "delete mapping", method: http.MethodDelete, path: "/api/v1/mappings/cand", wantResource: "mappings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &fakeControlPlaneAuditMetrics{}
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			mw := AuditControlPlaneMutations(logger, metrics)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			mw(next).ServeHTTP(rr, req)

			assert.Equal(t, []string{tt.wantResource + "|" + tt.method + "|success"}, metrics.snapshot())
			assert.Contains(t, buf.String(), "control plane mutation")
			assert.Contains(t, buf.String(), tt.wantResource)
		})
	}
}

// TestAuditControlPlaneMutations_RecordsErrorOutcome proves a rejected
// mutation (a non-2xx response) is recorded with outcome=error, not
// conflated with an actual state change.
func TestAuditControlPlaneMutations_RecordsErrorOutcome(t *testing.T) {
	metrics := &fakeControlPlaneAuditMetrics{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := AuditControlPlaneMutations(logger, metrics)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/stages/missing", nil)
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)

	assert.Equal(t, []string{"stages|PATCH|error"}, metrics.snapshot())
}

// TestAuditControlPlaneMutations_RecordsPanicAsErrorThenRePanics proves
// a handler panic still leaves an audit trail before propagating, so
// the outer recoverer still sees and handles the panic as before.
func TestAuditControlPlaneMutations_RecordsPanicAsErrorThenRePanics(t *testing.T) {
	metrics := &fakeControlPlaneAuditMetrics{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := AuditControlPlaneMutations(logger, metrics)
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/backends/gpt4o", nil)
	rr := httptest.NewRecorder()

	assert.PanicsWithValue(t, "boom", func() {
		mw(next).ServeHTTP(rr, req)
	})
	assert.Equal(t, []string{"backends|DELETE|error"}, metrics.snapshot())
}

func TestAuditControlPlaneMutations_IgnoresNonControlPlanePaths(t *testing.T) {
	metrics := &fakeControlPlaneAuditMetrics{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := AuditControlPlaneMutations(logger, metrics)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)

	assert.Empty(t, metrics.snapshot())
}
