package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/mappings"
	"github.com/harvard-cns/orla/internal/metrics"
	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/stages"
)

// TestRegisterControlPlaneRoutes_WiresAuditMiddleware exercises
// registerControlPlaneRoutes directly: a bare chi router, a real
// scheduler, and fake registries, proving it mounts all four
// control-plane groups with the audit middleware attached and wires
// the right dependency to each, without needing a database, an HTTP
// listener, or the rest of NewServer's middleware chain.
func TestRegisterControlPlaneRoutes_WiresAuditMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	promReg := prometheus.NewRegistry()
	m := metrics.New(promReg)

	sched := scheduler.New(nil, logger)
	t.Cleanup(func() { _ = sched.Shutdown(context.Background()) })

	stageReg := stages.NewFakeRegistry()
	mappingReg := mappings.NewFakeRegistry()
	backendReg := backends.NewFakeRegistry()
	policyStore := &settings.FakePolicyStore{}

	r := chi.NewRouter()
	registerControlPlaneRoutes(r, controlPlaneDeps{
		Logger:      logger,
		Metrics:     m,
		Stages:      stageReg,
		Mappings:    mappingReg,
		Backends:    backendReg,
		PolicyStore: policyStore,
		Scheduler:   sched,
	})

	tests := []struct {
		name         string
		method       string
		path         string
		body         map[string]any
		wantStatus   int
		wantResource string
	}{
		{
			name:         "put stage",
			method:       http.MethodPut,
			path:         "/api/v1/stages/planning",
			body:         map[string]any{"backend": "gpt-4o"},
			wantStatus:   http.StatusOK,
			wantResource: "stages",
		},
		{
			name:   "post mapping",
			method: http.MethodPost,
			path:   "/api/v1/mappings",
			body: map[string]any{
				"name":      "cand",
				"overrides": map[string]string{"planning": "gpt-4o-mini"},
			},
			wantStatus:   http.StatusCreated,
			wantResource: "mappings",
		},
		{
			name:         "put scheduler policy",
			method:       http.MethodPut,
			path:         "/api/v1/scheduler/policy",
			body:         map[string]any{"url": ""},
			wantStatus:   http.StatusOK,
			wantResource: "scheduler",
		},
		{
			name:   "post backend",
			method: http.MethodPost,
			path:   "/api/v1/backends",
			body: map[string]any{
				"name":            "gpt4o",
				"endpoint":        "https://api.openai.com/v1",
				"model_id":        "openai:gpt-4o",
				"max_concurrency": 4,
			},
			wantStatus:   http.StatusCreated,
			wantResource: "backends",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.body)
			require.NoError(t, err)
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			require.Equal(t, tt.wantStatus, rr.Code, rr.Body.String())
			got := testutil.ToFloat64(m.ControlPlaneMutationsTotal.WithLabelValues(tt.wantResource, tt.method, "success"))
			assert.InDelta(t, 1.0, got, 1e-9)
		})
	}
}
