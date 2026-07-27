package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/wire"
)

// CostDeps bundles the cost-policy handler dependencies.
type CostDeps struct {
	Store settings.CostStore
}

// RegisterCostRoutes mounts the cost-policy endpoints onto r. Routes:
//
//	GET /api/v1/costs/policy  read the active cost policy
//	PUT /api/v1/costs/policy  set the refresh interval
func RegisterCostRoutes(r chi.Router, deps CostDeps) {
	h := &costHandler{deps: deps}
	r.Route("/api/v1/costs/policy", func(r chi.Router) {
		r.Get("/", h.get)
		r.Put("/", h.set)
	})
}

type costHandler struct {
	deps CostDeps
}

func (h *costHandler) get(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.deps.Store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, costToWire(cfg))
}

func (h *costHandler) set(w http.ResponseWriter, r *http.Request) {
	var req wire.CostPolicy
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshIntervalMS <= 0 {
		writeErrorMsg(w, http.StatusBadRequest, "refresh_interval_ms must be > 0")
		return
	}
	cfg := settings.CostConfig{
		RefreshInterval: time.Duration(req.RefreshIntervalMS) * time.Millisecond,
	}
	if err := h.deps.Store.Set(r.Context(), cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, costToWire(cfg))
}

func costToWire(cfg settings.CostConfig) wire.CostPolicy {
	return wire.CostPolicy{RefreshIntervalMS: int(cfg.Interval().Milliseconds())}
}
