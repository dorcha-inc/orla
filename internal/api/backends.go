package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/wire"
)

// BackendLifecycle is the subset of scheduler.Scheduler the backend
// handlers call to keep the runtime in sync with the registry. Pass
// nil in tests that don't exercise dispatch.
type BackendLifecycle interface {
	Register(b *backends.Backend)
	Deregister(name string)
}

// LLMBackendManager reads runtime circuit-breaker state from the scheduler.
// Defined as an interface rather than *scheduler.Scheduler directly so handler
// tests can substitute a controlled fake — testing specific states (open,
// half-open) without constructing a real scheduler and dispatching requests
// to trip the circuit.
type LLMBackendManager interface {
	CircuitState(name string) string
}

// BackendDeps bundles backend-handler dependencies.
type BackendDeps struct {
	Registry  backends.Registry
	Lifecycle BackendLifecycle
	Manager   LLMBackendManager

	// AuditMiddleware audits control-plane mutations. Nil disables it.
	AuditMiddleware func(http.Handler) http.Handler
}

// backendView is the HTTP response shape for a single backend. It embeds
// the persisted Backend record and adds runtime fields not stored in the DB.
type backendView struct {
	*backends.Backend
	CircuitBreaker string `json:"circuit_breaker"`
}

// RegisterBackendRoutes mounts the backend endpoints onto r. Routes:
//
//	POST   /api/v1/backends         create with name in the body
//	GET    /api/v1/backends         list
//	GET    /api/v1/backends/{name}  read one
//	PATCH  /api/v1/backends/{name}  partial update
//	DELETE /api/v1/backends/{name}  remove
//
// There is no PUT. Backends are created with POST. To change name or
// model_id, delete and re-create.
func RegisterBackendRoutes(r chi.Router, deps BackendDeps) {
	h := &backendHandler{deps: deps}
	r.Route("/api/v1/backends", func(r chi.Router) {
		if deps.AuditMiddleware != nil {
			r.Use(deps.AuditMiddleware)
		}
		r.Post("/", h.create)
		r.Get("/", h.list)
		r.Route("/{name}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Patch("/", h.patch)
			r.Delete("/", h.delete)
		})
	})
}

type backendHandler struct {
	deps BackendDeps
}

func (h *backendHandler) create(w http.ResponseWriter, r *http.Request) {
	var req wire.CreateBackendRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Endpoint == "" {
		writeErrorMsg(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	if req.MaxConcurrency < 1 {
		writeErrorMsg(w, http.StatusBadRequest, "max_concurrency must be >= 1")
		return
	}
	if req.RatePerSecond != nil && *req.RatePerSecond < 0 {
		writeErrorMsg(w, http.StatusBadRequest, "rate_per_second must be >= 0")
		return
	}
	if req.InputCostPerMtoken != nil && !isFiniteNonNegative(*req.InputCostPerMtoken) {
		writeErrorMsg(w, http.StatusBadRequest, "input_cost_per_mtoken must be a non-negative finite number")
		return
	}
	if req.OutputCostPerMtoken != nil && !isFiniteNonNegative(*req.OutputCostPerMtoken) {
		writeErrorMsg(w, http.StatusBadRequest, "output_cost_per_mtoken must be a non-negative finite number")
		return
	}
	if msg := validateRates(req.Rates); msg != "" {
		writeErrorMsg(w, http.StatusBadRequest, msg)
		return
	}

	kind := backends.Kind(req.Kind)
	if kind == "" {
		kind = backends.KindLLM
	}
	switch kind {
	case backends.KindLLM:
		if req.ModelID == "" {
			writeErrorMsg(w, http.StatusBadRequest, "model_id is required for kind=llm")
			return
		}
		if len(req.Rates) > 0 {
			writeErrorMsg(w, http.StatusBadRequest,
				"rates is only valid for kind=tool; LLM backends use input_cost_per_mtoken and output_cost_per_mtoken")
			return
		}
		if req.CostSource != "" {
			if err := validateHTTPURL(req.CostSource); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("cost_source %w", err))
				return
			}
		}
	case backends.KindTool:
		if req.ToolKind == "" {
			writeErrorMsg(w, http.StatusBadRequest, "tool_kind is required for kind=tool")
			return
		}
		if req.CostSource != "" {
			writeErrorMsg(w, http.StatusBadRequest, "cost_source is only valid for kind=llm")
			return
		}
	default:
		writeErrorMsg(w, http.StatusBadRequest,
			"kind must be 'llm' or 'tool'")
		return
	}

	b := &backends.Backend{
		Name:                req.Name,
		Endpoint:            req.Endpoint,
		APIKeyEnvVar:        req.APIKeyEnvVar,
		MaxConcurrency:      req.MaxConcurrency,
		InputCostPerMtoken:  req.InputCostPerMtoken,
		OutputCostPerMtoken: req.OutputCostPerMtoken,
		Quality:             req.Quality,
		RatePerSecond:       req.RatePerSecond,
		Kind:                kind,
		Rates:               req.Rates,
	}
	if req.ModelID != "" {
		b.ModelID = &req.ModelID
	}
	if req.ToolKind != "" {
		b.ToolKind = &req.ToolKind
	}
	if req.CostSource != "" {
		b.CostSource = &req.CostSource
	}
	b, err := h.deps.Registry.Insert(r.Context(), b)
	if err != nil {
		if errors.Is(err, backends.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if h.deps.Lifecycle != nil {
		h.deps.Lifecycle.Register(b)
	}
	writeJSON(w, http.StatusCreated, b)
}

func (h *backendHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.deps.Registry.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]backendView, len(rows))
	for i, b := range rows {
		views[i] = backendView{Backend: b, CircuitBreaker: h.circuitState(b.Name)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"backends": views})
}

func (h *backendHandler) get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	b, err := h.deps.Registry.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, backends.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, backendView{Backend: b, CircuitBreaker: h.circuitState(name)})
}

func (h *backendHandler) circuitState(name string) string {
	if h.deps.Manager == nil {
		return "closed"
	}
	return h.deps.Manager.CircuitState(name)
}

func (h *backendHandler) patch(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req backends.PatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.MaxConcurrency != nil && *req.MaxConcurrency < 1 {
		writeErrorMsg(w, http.StatusBadRequest, "max_concurrency must be >= 1")
		return
	}
	if req.RatePerSecond != nil && *req.RatePerSecond < 0 {
		writeErrorMsg(w, http.StatusBadRequest, "rate_per_second must be >= 0")
		return
	}
	if req.InputCostPerMtoken != nil && !isFiniteNonNegative(*req.InputCostPerMtoken) {
		writeErrorMsg(w, http.StatusBadRequest, "input_cost_per_mtoken must be a non-negative finite number")
		return
	}
	if req.OutputCostPerMtoken != nil && !isFiniteNonNegative(*req.OutputCostPerMtoken) {
		writeErrorMsg(w, http.StatusBadRequest, "output_cost_per_mtoken must be a non-negative finite number")
		return
	}
	if req.Rates != nil {
		if msg := validateRates(*req.Rates); msg != "" {
			writeErrorMsg(w, http.StatusBadRequest, msg)
			return
		}
	}
	if req.CostSource != nil && *req.CostSource != "" {
		if err := validateHTTPURL(*req.CostSource); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("cost_source %w", err))
			return
		}
		current, err := h.deps.Registry.Get(r.Context(), name)
		if err != nil {
			if errors.Is(err, backends.ErrNotFound) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if current.Kind != backends.KindLLM {
			writeErrorMsg(w, http.StatusBadRequest, "cost_source is only valid for kind=llm")
			return
		}
	}
	b, err := h.deps.Registry.Patch(r.Context(), name, req)
	if err != nil {
		if errors.Is(err, backends.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Re-register so the scheduler picks up changes to MaxConcurrency,
	// endpoint, or API key env var.
	if h.deps.Lifecycle != nil {
		h.deps.Lifecycle.Register(b)
	}
	writeJSON(w, http.StatusOK, b)
}

func (h *backendHandler) delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.deps.Registry.Delete(r.Context(), name); err != nil {
		if errors.Is(err, backends.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if h.deps.Lifecycle != nil {
		h.deps.Lifecycle.Deregister(name)
	}
	w.WriteHeader(http.StatusNoContent)
}
