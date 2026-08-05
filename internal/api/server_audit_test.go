package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// newAuditTestServer enables the audit through the real NewServer and
// registers one route per shape the control plane uses.
func newAuditTestServer(t *testing.T, logger *slog.Logger) (*Server, *fakeControlPlaneAuditMetrics) {
	t.Helper()
	m := &fakeControlPlaneAuditMetrics{}
	srv := NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        logger,
		AuditMetrics:  m,
	})
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	r := srv.Router()
	r.Route("/api/v1/stages/{id}", func(r chi.Router) {
		r.Get("/", ok)
		r.Head("/", ok)
		r.Put("/", ok)
		r.Patch("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		r.Delete("/", func(http.ResponseWriter, *http.Request) { panic("boom") })
		// net/http sends 200 for a handler that writes nothing.
		r.Post("/", func(http.ResponseWriter, *http.Request) {})
	})
	r.Post("/api/v1/scheduler/policy", ok)
	r.Post("/v1/chat/completions", ok)
	return srv, m
}

func TestControlPlaneResource_Outcomes(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
		wantOK  bool
	}{
		{name: "resource with id", pattern: "/api/v1/stages/{id}", want: "stages", wantOK: true},
		{name: "list, no id", pattern: "/api/v1/stages", want: "stages", wantOK: true},
		{name: "list, trailing slash", pattern: "/api/v1/stages/", want: "stages", wantOK: true},
		{name: "backends resource", pattern: "/api/v1/backends/{name}", want: "backends", wantOK: true},
		{name: "hyphenated resource", pattern: "/api/v1/stage-mapper/", want: "stage-mapper", wantOK: true},
		{name: "nested scheduler policy path", pattern: "/api/v1/scheduler/policy", want: "scheduler", wantOK: true},
		{name: "wildcard under a resource", pattern: "/api/v1/stages/{id}/*", want: "stages", wantOK: true},
		{name: "not a control-plane path", pattern: "/v1/chat/completions", want: "", wantOK: false},
		{name: "prefix only, no resource", pattern: "/api/v1/", want: "", wantOK: false},
		{name: "empty first segment", pattern: "/api/v1//policy", want: "", wantOK: false},
		{name: "unrouted request, no pattern", pattern: "", want: "", wantOK: false},
		{name: "root", pattern: "/", want: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := controlPlaneResource(tt.pattern)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestAuditControlPlaneMutations_SkipsReads(t *testing.T) {
	srv, m := newAuditTestServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, httptest.NewRequest(method, "/api/v1/stages/planning", nil))
		require.Equal(t, http.StatusOK, rr.Code)
	}

	assert.Empty(t, m.snapshot())
}

func TestAuditControlPlaneMutations_RecordsMutations(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{name: "put stage", method: http.MethodPut, path: "/api/v1/stages/planning", want: "stages|PUT|success"},
		// Every orla handler writes a status. A handler that does not
		// counts as an error, since the recorded status stays at zero.
		{name: "handler writes no status", method: http.MethodPost, path: "/api/v1/stages/planning", want: "stages|POST|error"},
		{name: "post scheduler policy", method: http.MethodPost, path: "/api/v1/scheduler/policy", want: "scheduler|POST|success"},
		{name: "rejected patch", method: http.MethodPatch, path: "/api/v1/stages/planning", want: "stages|PATCH|error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, m := newAuditTestServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)))

			rr := httptest.NewRecorder()
			srv.Router().ServeHTTP(rr, httptest.NewRequest(tt.method, tt.path, nil))

			assert.Equal(t, []string{tt.want}, m.snapshot())
		})
	}
}

// TestAuditControlPlaneMutations_RecordsPanicAsError covers a handler
// that panics mid-mutation. The audit runs outside Recoverer, so it
// records the attempt with the 500 the recoverer produced.
func TestAuditControlPlaneMutations_RecordsPanicAsError(t *testing.T) {
	var buf bytes.Buffer
	srv, m := newAuditTestServer(t, slog.New(slog.NewTextHandler(&buf, nil)))

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/stages/planning", nil))

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, []string{"stages|DELETE|error"}, m.snapshot())
	assert.Contains(t, buf.String(), "status=500")
}

func TestAuditControlPlaneMutations_IgnoresDataPlane(t *testing.T) {
	srv, m := newAuditTestServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)))

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, m.snapshot())
}

// TestAuditControlPlaneMutations_IgnoresUnroutedPaths keeps the metric's
// label set bounded. The resource label comes from the URL, so counting
// requests that matched no route would let any caller mint an unbounded
// number of label series by POSTing junk paths.
func TestAuditControlPlaneMutations_IgnoresUnroutedPaths(t *testing.T) {
	srv, m := newAuditTestServer(t, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, path := range []string{"/api/v1/junk", "/api/v1/other", "/api/v1/"} {
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusNotFound, rr.Code, path)
	}

	assert.Empty(t, m.snapshot())
}

// TestAuditControlPlaneMutations_ServesWithoutMetrics covers a server
// built without an audit sink, which must serve control-plane writes
// rather than dereference the absent interface.
func TestAuditControlPlaneMutations_ServesWithoutMetrics(t *testing.T) {
	srv := NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv.Router().Put("/api/v1/stages/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/v1/stages/planning", nil))

	assert.Equal(t, http.StatusOK, rr.Code)
}
