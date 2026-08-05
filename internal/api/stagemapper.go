package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/mappings"
	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/wire"
)

// defaultMapperTimeout is used when a caller sets a stage mapper
// without a positive timeout.
const defaultMapperTimeout = 50 * time.Millisecond

// StageMapperDeps bundles the stage-mapper handler dependencies.
type StageMapperDeps struct {
	Store  settings.MapperStore
	Holder *mappings.MapperHolder
}

// RegisterStageMapperRoutes mounts the stage-mapper endpoints onto
// r. Routes:
//
//	GET /api/v1/stage-mapper  read the active stage mapper
//	PUT /api/v1/stage-mapper  set the mapper, empty url disables it
func RegisterStageMapperRoutes(r chi.Router, deps StageMapperDeps) {
	h := &stageMapperHandler{deps: deps}
	r.Route("/api/v1/stage-mapper", func(r chi.Router) {
		r.Get("/", h.get)
		r.Put("/", h.set)
	})
}

type stageMapperHandler struct {
	deps StageMapperDeps
}

func (h *stageMapperHandler) get(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.deps.Store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stageMapperToWire(cfg))
}

func (h *stageMapperHandler) set(w http.ResponseWriter, r *http.Request) {
	var req wire.StageMapper
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL != "" {
		if err := validateHTTPURL(req.URL); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("url %w", err))
			return
		}
	}
	if req.TimeoutMS < 0 {
		writeErrorMsg(w, http.StatusBadRequest, "timeout_ms must not be negative")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultMapperTimeout
	}
	cfg := settings.MapperConfig{URL: req.URL, Timeout: timeout}
	if err := h.deps.Store.Set(r.Context(), cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ApplyStageMapper(h.deps.Holder, cfg)
	writeJSON(w, http.StatusOK, stageMapperToWire(cfg))
}

// ApplyStageMapper builds the stage mapper described by cfg and
// installs it on the holder. It is shared by the PUT handler and the
// daemon boot path so both interpret a stored mapper the same way.
func ApplyStageMapper(holder *mappings.MapperHolder, cfg settings.MapperConfig) {
	if !cfg.Enabled() {
		holder.Set(nil)
		return
	}
	holder.Set(mappings.NewHTTPMapper(cfg.URL, cfg.Timeout))
}

func stageMapperToWire(cfg settings.MapperConfig) wire.StageMapper {
	return wire.StageMapper{
		URL:       cfg.URL,
		TimeoutMS: int(cfg.Timeout.Milliseconds()),
		Enabled:   cfg.Enabled(),
	}
}
