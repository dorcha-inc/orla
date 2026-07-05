package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/telemetry"
)

// MapperReader is the subset of telemetry.Reader the mapper endpoints
// call. Tests can supply a fake.
type MapperReader interface {
	ListStageCompletions(ctx context.Context, stageID string, since time.Time, limit int32) ([]*telemetry.CompletionRecord, error)
	ListStageFeedback(ctx context.Context, stageID string, since time.Time, limit int32) ([]*telemetry.Feedback, error)
	StageMetrics(ctx context.Context, stageID string, since time.Time) ([]*telemetry.CompletionMetrics, error)
	CostByMapping(ctx context.Context, since time.Time) ([]*telemetry.MappingCost, error)
}

// MapperDeps bundles the mapper read-endpoint dependencies.
type MapperDeps struct {
	Reader MapperReader
}

const (
	defaultListLimit = 100
	maxListLimit     = 1000
)

// RegisterMapperRoutes mounts the mapper read endpoints onto r. Routes:
//
//	GET /api/v1/stages/{id}/completions?since=&limit=
//	GET /api/v1/stages/{id}/feedback?since=&limit=
//	GET /api/v1/stages/{id}/metrics?since=
//
// These are registered as flat paths rather than a subrouter under
// /api/v1/stages/{id} so they don't collide with the stage CRUD routes
// in stages.go.
func RegisterMapperRoutes(r chi.Router, deps MapperDeps) {
	h := &mapperHandler{deps: deps}
	r.Get("/api/v1/stages/{id}/completions", h.listCompletions)
	r.Get("/api/v1/stages/{id}/feedback", h.listFeedback)
	r.Get("/api/v1/stages/{id}/metrics", h.metrics)
	r.Get("/api/v1/costs", h.costByMapping)
}

type mapperHandler struct {
	deps MapperDeps
}

func (h *mapperHandler) listCompletions(w http.ResponseWriter, r *http.Request) {
	stageID := chi.URLParam(r, "id")
	since, ok := parseSince(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	rows, err := h.deps.Reader.ListStageCompletions(r.Context(), stageID, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completions": rows})
}

func (h *mapperHandler) listFeedback(w http.ResponseWriter, r *http.Request) {
	stageID := chi.URLParam(r, "id")
	since, ok := parseSince(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(w, r)
	if !ok {
		return
	}
	rows, err := h.deps.Reader.ListStageFeedback(r.Context(), stageID, since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedback": rows})
}

func (h *mapperHandler) metrics(w http.ResponseWriter, r *http.Request) {
	stageID := chi.URLParam(r, "id")
	since, ok := parseSince(w, r)
	if !ok {
		return
	}
	rows, err := h.deps.Reader.StageMetrics(r.Context(), stageID, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": rows})
}

// costByMapping serves GET /api/v1/costs?since=, total cost and
// dispatch count grouped by mapping variant. The live critical path is
// the row with an empty mapping.
func (h *mapperHandler) costByMapping(w http.ResponseWriter, r *http.Request) {
	since, ok := parseSince(w, r)
	if !ok {
		return
	}
	rows, err := h.deps.Reader.CostByMapping(r.Context(), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"costs": rows})
}

// parseSince reads ?since=<rfc3339>. Empty means no filter (zero time).
func parseSince(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest,
			"since must be an RFC3339 timestamp (e.g., 2026-01-02T15:04:05Z)")
		return time.Time{}, false
	}
	return t, true
}

// parseLimit reads ?limit=<int>. Defaults and caps live in this file's
// constants.
func parseLimit(w http.ResponseWriter, r *http.Request) (int32, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultListLimit, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "limit must be an integer")
		return 0, false
	}
	if v < 1 {
		writeErrorMsg(w, http.StatusBadRequest, "limit must be >= 1")
		return 0, false
	}
	if v > maxListLimit {
		v = maxListLimit
	}
	return int32(v), true //nolint:gosec // bounded above by maxListLimit (1000) and below by >=1 checks
}
