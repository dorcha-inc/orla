package api

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/wire"
)

// defaultPolicyTimeout is used when a caller sets a policy without a
// positive timeout.
const defaultPolicyTimeout = 50 * time.Millisecond

// SchedulerPolicySetter installs a scheduling policy on the running
// scheduler. *scheduler.Scheduler satisfies it.
type SchedulerPolicySetter interface {
	SetPolicy(policy scheduler.SchedulingPolicy, timeout time.Duration)
}

// SchedulerDeps bundles the scheduler-policy handler dependencies.
type SchedulerDeps struct {
	Store     settings.PolicyStore
	Scheduler SchedulerPolicySetter

	// AuditMiddleware audits control-plane mutations. Nil disables it.
	AuditMiddleware func(http.Handler) http.Handler
}

// RegisterSchedulerRoutes mounts the scheduling-policy endpoints onto r.
// Routes:
//
//	GET /api/v1/scheduler/policy  read the active policy
//	PUT /api/v1/scheduler/policy  set the policy, empty url disables it
func RegisterSchedulerRoutes(r chi.Router, deps SchedulerDeps) {
	h := &schedulerHandler{deps: deps}
	r.Route("/api/v1/scheduler/policy", func(r chi.Router) {
		if deps.AuditMiddleware != nil {
			r.Use(deps.AuditMiddleware)
		}
		r.Get("/", h.get)
		r.Put("/", h.set)
	})
}

type schedulerHandler struct {
	deps SchedulerDeps
}

func (h *schedulerHandler) get(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.deps.Store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, policyToWire(cfg))
}

func (h *schedulerHandler) set(w http.ResponseWriter, r *http.Request) {
	var req wire.SchedulerPolicy
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL != "" {
		if err := validateSchedulerURL(req.URL); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.TimeoutMS < 0 {
		writeErrorMsg(w, http.StatusBadRequest, "timeout_ms must not be negative")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultPolicyTimeout
	}
	cfg := settings.PolicyConfig{URL: req.URL, Timeout: timeout}
	if err := h.deps.Store.Set(r.Context(), cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ApplySchedulerPolicy(h.deps.Scheduler, cfg)
	writeJSON(w, http.StatusOK, policyToWire(cfg))
}

// ApplySchedulerPolicy builds the scheduling policy described by cfg and
// installs it on the scheduler. It is shared by the PUT handler and the
// daemon boot path so both interpret a stored policy the same way.
func ApplySchedulerPolicy(s SchedulerPolicySetter, cfg settings.PolicyConfig) {
	var p scheduler.SchedulingPolicy
	if cfg.Enabled() {
		p = scheduler.NewHTTPPolicy(cfg.URL, cfg.Timeout)
	}
	s.SetPolicy(p, cfg.Timeout)
}

func policyToWire(cfg settings.PolicyConfig) wire.SchedulerPolicy {
	return wire.SchedulerPolicy{
		URL:       cfg.URL,
		TimeoutMS: int(cfg.Timeout.Milliseconds()),
		Enabled:   cfg.Enabled(),
	}
}

// validateSchedulerURL rejects a URL orla could never POST a decision
// to, so a bad value is caught at the API boundary rather than falling
// back to fcfs on every request without anyone noticing.
func validateSchedulerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url is not valid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("url must include a host, got %q", raw)
	}
	return nil
}
