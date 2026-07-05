package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/telemetry"
)

type fakeReader struct {
	mu          sync.Mutex
	completions map[string][]*telemetry.CompletionRecord
	feedback    map[string][]*telemetry.Feedback
	metrics     map[string][]*telemetry.CompletionMetrics
	costs       []*telemetry.MappingCost
	err         error
}

func (f *fakeReader) ListStageCompletions(_ context.Context, stage string, since time.Time, limit int32) ([]*telemetry.CompletionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	all := f.completions[stage]
	var out []*telemetry.CompletionRecord
	for _, r := range all {
		if !since.IsZero() && !r.CreatedAt.After(since) {
			continue
		}
		out = append(out, r)
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeReader) ListStageFeedback(_ context.Context, stage string, since time.Time, limit int32) ([]*telemetry.Feedback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	all := f.feedback[stage]
	var out []*telemetry.Feedback
	for _, r := range all {
		if !since.IsZero() && !r.CreatedAt.After(since) {
			continue
		}
		out = append(out, r)
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeReader) StageMetrics(_ context.Context, stage string, _ time.Time) ([]*telemetry.CompletionMetrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.metrics[stage], nil
}

func (f *fakeReader) CostByMapping(_ context.Context, _ time.Time) ([]*telemetry.MappingCost, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.costs, nil
}

func newMapperTestServer(t *testing.T) (*Server, *fakeReader) {
	t.Helper()
	r := &fakeReader{
		completions: map[string][]*telemetry.CompletionRecord{},
		feedback:    map[string][]*telemetry.Feedback{},
		metrics:     map[string][]*telemetry.CompletionMetrics{},
	}
	srv := NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	RegisterMapperRoutes(srv.Router(), MapperDeps{Reader: r})
	return srv, r
}

func TestMapper_ListCompletions(t *testing.T) {
	srv, r := newMapperTestServer(t)
	r.completions["planning"] = []*telemetry.CompletionRecord{
		{CompletionID: "a", StageID: "planning", Backend: "gpt4o", Status: "success", CreatedAt: time.Now()},
		{CompletionID: "b", StageID: "planning", Backend: "gpt4o", Status: "success", CreatedAt: time.Now()},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/completions", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Completions []*telemetry.CompletionRecord `json:"completions"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Len(t, body.Completions, 2)
}

func TestMapper_ListCompletions_RespectsLimit(t *testing.T) {
	srv, r := newMapperTestServer(t)
	for range 5 {
		r.completions["planning"] = append(r.completions["planning"],
			&telemetry.CompletionRecord{CompletionID: "x", CreatedAt: time.Now()})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/completions?limit=2", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	var body struct {
		Completions []*telemetry.CompletionRecord `json:"completions"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Len(t, body.Completions, 2)
}

func TestMapper_ListCompletions_BadSince(t *testing.T) {
	srv, _ := newMapperTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/completions?since=not-a-date", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMapper_ListCompletions_BadLimit(t *testing.T) {
	srv, _ := newMapperTestServer(t)
	for _, bad := range []string{"foo", "-1", "0"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/completions?limit="+bad, nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code, "limit=%q should be rejected", bad)
	}
}

func TestMapper_ListFeedback(t *testing.T) {
	srv, r := newMapperTestServer(t)
	r.feedback["planning"] = []*telemetry.Feedback{
		{ID: 1, CompletionID: "a", StageID: "planning", CreatedAt: time.Now()},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/feedback", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Feedback []*telemetry.Feedback `json:"feedback"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Feedback, 1)
	assert.Equal(t, int64(1), body.Feedback[0].ID)
}

func TestMapper_Metrics(t *testing.T) {
	srv, r := newMapperTestServer(t)
	r.metrics["planning"] = []*telemetry.CompletionMetrics{
		{Backend: "gpt4o", Count: 10, AvgLatencyMs: 120, P50LatencyMs: 100, P95LatencyMs: 250, TotalCostUSD: 0.5},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/metrics", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Metrics []*telemetry.CompletionMetrics `json:"metrics"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Metrics, 1)
	assert.Equal(t, "gpt4o", body.Metrics[0].Backend)
	assert.Equal(t, int64(10), body.Metrics[0].Count)
}

func TestMapper_CostByMapping(t *testing.T) {
	srv, r := newMapperTestServer(t)
	r.costs = []*telemetry.MappingCost{
		{Mapping: "", Count: 2, TotalCostUSD: 0.03},
		{Mapping: "cand", Count: 1, TotalCostUSD: 0.05},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/costs", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Costs []*telemetry.MappingCost `json:"costs"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Costs, 2)
	assert.Equal(t, "cand", body.Costs[1].Mapping)
	assert.InDelta(t, 0.05, body.Costs[1].TotalCostUSD, 1e-9)
}

func TestMapper_CostByMapping_BadSince(t *testing.T) {
	srv, _ := newMapperTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/costs?since=notatime", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMapper_BubblesReaderError(t *testing.T) {
	srv, r := newMapperTestServer(t)
	r.err = errors.New("simulated")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/metrics", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestMapper_LimitCappedAtMax(t *testing.T) {
	srv, r := newMapperTestServer(t)
	for range 1500 {
		r.completions["planning"] = append(r.completions["planning"],
			&telemetry.CompletionRecord{CompletionID: "x", CreatedAt: time.Now()})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stages/planning/completions?limit=10000", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var body struct {
		Completions []*telemetry.CompletionRecord `json:"completions"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.LessOrEqual(t, len(body.Completions), maxListLimit)
}
