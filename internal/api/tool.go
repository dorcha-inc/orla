// Tool HTTP route: POST /v1/tools/{kind}
//
// The wire shape mirrors the OpenAI-compatible chat path. The developer
// hands orla a request. Orla routes by stage, records the completion
// with its cost, and returns the response. For tool dispatches the
// request and response are kind-specific JSON payloads. Orla stays
// agnostic to their shapes and only routes, accounts, and feeds the
// mapper.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/provider"
	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/stages"
	"github.com/harvard-cns/orla/internal/telemetry"
)

// ToolDeps bundles the tool handler's dependencies. Parallels ProxyDeps.
type ToolDeps struct {
	Stages         stages.Registry
	Scheduler      *scheduler.Scheduler
	Backends       backends.Registry
	CompletionSink CompletionSink
	Metrics        ProxyMetrics
}

// RegisterToolRoutes mounts POST /v1/tools/{kind}.
func RegisterToolRoutes(r chi.Router, deps ToolDeps) {
	h := &toolHandler{deps: deps}
	r.Post("/v1/tools/{kind}", h.invoke)
}

type toolHandler struct {
	deps ToolDeps
}

// invoke is the POST /v1/tools/{kind} handler. The flow is:
//
//  1. Parse the URL kind and extract the request context.
//  2. Resolve the stage to a backend via the stages registry.
//  3. Look up the backend record to read its Kind and rates.
//  4. Verify backend.Kind is "tool" and backend.ToolKind matches the URL.
//  5. Acquire a worker slot via the scheduler.
//  6. Decode the body into a provider.ToolRequest and dispatch.
//  7. Record the completion, emit metrics, and return the response.
func (h *toolHandler) invoke(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	if kind == "" {
		writeErrorMsg(w, http.StatusBadRequest, "tool kind is required in URL path")
		return
	}

	rc := extractRequestContext(r, nil)
	if rc.Stage == "" {
		writeErrorMsg(w, http.StatusBadRequest,
			"stage is required (set X-Orla-Stage)")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read body: %w", err))
		return
	}

	stage, err := h.deps.Stages.GetOrCreate(r.Context(), rc.Stage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	backendName := stage.Backend
	if backendName == "" {
		writeErrorMsg(w, http.StatusBadRequest,
			fmt.Sprintf("stage %q has no backend mapping", rc.Stage))
		return
	}

	// The backend record carries Kind, ToolKind, and the cost rates.
	bk, err := h.deps.Backends.Get(r.Context(), backendName)
	if err != nil {
		if errors.Is(err, backends.ErrNotFound) {
			writeError(w, http.StatusBadGateway,
				fmt.Errorf("backend %q is not registered", backendName))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if bk.Kind != backends.KindTool {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("backend %q is kind=%q; the /v1/tools route requires kind=tool",
				bk.Name, bk.Kind))
		return
	}
	if bk.ToolKind == nil || *bk.ToolKind != kind {
		got := ""
		if bk.ToolKind != nil {
			got = *bk.ToolKind
		}
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("backend %q has tool_kind=%q; request asked for %q",
				bk.Name, got, kind))
		return
	}

	tp, release, err := h.deps.Scheduler.AcquireTool(r.Context(), backendName)
	if err != nil {
		statusForSchedulerErr(w, err, backendName, h.deps.Metrics)
		return
	}
	defer release()

	req := provider.ToolRequest{
		Kind:    kind,
		Payload: body,
	}

	completionID := uuid.NewString()

	start := time.Now()
	resp, err := tp.Invoke(r.Context(), req)
	latencyMs := int(time.Since(start) / time.Millisecond)
	h.deps.Scheduler.ReportOutcome(backendName, err)

	if err != nil {
		h.recordToolCompletion(&toolCompletionInputs{
			completionID: completionID,
			rc:           rc,
			backend:      backendName,
			toolKind:     kind,
			status:       "error",
			latencyMs:    &latencyMs,
		})
		h.emitMetrics(rc.Stage, backendName, "error", latencyMs)
		writeError(w, http.StatusBadGateway, err)
		return
	}

	// Compute cost. If the tool reported its own cost directly, use
	// that. Otherwise sum the dot product of reported usage with the
	// backend's rates.
	costUSD := computeToolCost(resp, bk.Rates, bk.Name, completionID)

	// Copy resp.Usage so the async telemetry writer can read it
	// without aliasing the provider's response. Today's providers
	// allocate a fresh map per Invoke; future providers might pool
	// or reuse maps, and the copy makes the boundary explicit.
	h.recordToolCompletion(&toolCompletionInputs{
		completionID: completionID,
		rc:           rc,
		backend:      backendName,
		toolKind:     kind,
		status:       "success",
		latencyMs:    &latencyMs,
		costUSD:      costUSD,
		usage:        maps.Clone(resp.Usage),
	})
	h.emitMetrics(rc.Stage, backendName, "success", latencyMs)

	// Return the response envelope verbatim so downstream callers see
	// the same shape the underlying ToolProvider returned.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Orla-Completion-Id", completionID)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Header is already on the wire so we cannot change the status
		// code. Log so operators can tell that the body is truncated.
		slog.Default().Warn("tool: encode response failed",
			"completion_id", completionID,
			"backend", backendName,
			"error", err.Error(),
		)
	}
}

type toolCompletionInputs struct {
	completionID string
	rc           *requestContext
	backend      string
	toolKind     string
	status       string
	latencyMs    *int
	costUSD      *float64
	usage        map[string]float64
}

func (h *toolHandler) recordToolCompletion(in *toolCompletionInputs) {
	if h.deps.CompletionSink == nil {
		return
	}
	_ = h.deps.CompletionSink.Submit(&telemetry.CompletionRecord{
		CompletionID: in.completionID,
		StageID:      in.rc.Stage,
		WorkflowRun:  in.rc.WorkflowRun,
		Backend:      in.backend,
		Status:       in.status,
		LatencyMs:    in.latencyMs,
		CostUSD:      in.costUSD,
		Usage:        in.usage,
		ToolKind:     in.toolKind,
		Tags:         in.rc.Tags,
		CreatedAt:    time.Now(),
	})
}

// computeToolCost rolls a tool response and a backend's rates into a
// single dollar amount. If the tool reported a cost directly, that
// value is used after a sanity check. Otherwise cost is the sum over
// each (key, amount) in Usage of amount times the matching
// Rates[key]. Returns nil when no usable cost signal is present.
//
// A reported CostUSD that is negative, NaN, or +/-Inf is dropped with
// a logged warning rather than recorded verbatim, so a buggy upstream
// cannot poison billing aggregates. The same drop policy applies to
// dot-product results that come out non-finite (which is only
// possible when usage or rate values are themselves non-finite, but
// validation should have caught those upstream).
//
// When Usage and Rates have no overlapping keys, the function logs a
// warning and returns nil. Silent zero would hide a misconfiguration
// where the tool's reported keys do not match what the platform
// engineer priced.
func computeToolCost(
	resp *provider.ToolResponse,
	rates map[string]float64,
	backendName, completionID string,
) *float64 {
	if resp == nil {
		return nil
	}
	if resp.CostUSD != nil {
		c := *resp.CostUSD
		if !isFiniteNonNegative(c) {
			slog.Default().Warn("tool: dropping non-finite or negative reported cost",
				"backend", backendName,
				"completion_id", completionID,
				"cost_usd", c,
			)
			return nil
		}
		return &c
	}
	if len(resp.Usage) == 0 || len(rates) == 0 {
		return nil
	}
	var total float64
	var matched bool
	for key, amount := range resp.Usage {
		if rate, ok := rates[key]; ok {
			total += amount * rate
			matched = true
		}
	}
	if !matched {
		slog.Default().Warn("tool: usage keys do not overlap backend rates",
			"backend", backendName,
			"completion_id", completionID,
			"usage_keys", slices.Sorted(maps.Keys(resp.Usage)),
			"rate_keys", slices.Sorted(maps.Keys(rates)),
		)
		return nil
	}
	if !isFiniteNonNegative(total) {
		slog.Default().Warn("tool: dot-product cost is non-finite or negative",
			"backend", backendName,
			"completion_id", completionID,
			"cost_usd", total,
		)
		return nil
	}
	return &total
}

func (h *toolHandler) emitMetrics(stage, backend, status string, latencyMs int) {
	if h.deps.Metrics == nil {
		return
	}
	h.deps.Metrics.IncRequest(stage, backend, status)
	h.deps.Metrics.ObserveBackendLatency(backend, float64(latencyMs)/1000.0)
}
