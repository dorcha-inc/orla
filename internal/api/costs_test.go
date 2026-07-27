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

	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/wire"
)

func newCostTestServer(t *testing.T) (*chi.Mux, *settings.FakeCostStore) {
	t.Helper()
	store := &settings.FakeCostStore{}
	r := chi.NewRouter()
	RegisterCostRoutes(r, CostDeps{Store: store})
	return r, store
}

func TestCostPolicy_GetDefaultsToOneMinute(t *testing.T) {
	r, _ := newCostTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/costs/policy", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var got wire.CostPolicy
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, int(settings.DefaultCostRefreshInterval.Milliseconds()), got.RefreshIntervalMS)
}

func TestCostPolicy_SetPersists(t *testing.T) {
	r, store := newCostTestServer(t)

	body := mustJSON(t, wire.CostPolicy{RefreshIntervalMS: 15000})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/costs/policy", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var got wire.CostPolicy
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, 15000, got.RefreshIntervalMS)

	stored, err := store.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, stored.Interval())
}

func TestCostPolicy_RejectsNonPositiveInterval(t *testing.T) {
	tests := []struct {
		name string
		ms   int
	}{
		{name: "zero", ms: 0},
		{name: "negative", ms: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newCostTestServer(t)
			body := mustJSON(t, wire.CostPolicy{RefreshIntervalMS: tt.ms})
			req := httptest.NewRequest(http.MethodPut, "/api/v1/costs/policy", bytes.NewReader(body))
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		})
	}
}
