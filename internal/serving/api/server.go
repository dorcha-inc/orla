// Package api provides the HTTP API for the serving layer daemon.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving"
	"github.com/harvard-cns/orla/internal/serving/access"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log/slog"
	"golang.org/x/time/rate"
)

// SSE event types for execute stream
const (
	sseEventContent  = "content"
	sseEventThinking = "thinking"
	sseEventDone     = "done"

	// maxRequestBodyBytes limits JSON request body size to prevent memory exhaustion (10MB)
	maxRequestBodyBytes = 10 << 20
	// readTimeout is the time we wait for the http client to send the request body
	readTimeout = 30 * time.Second
	// readHeaderTimeout is the time we wait for the http client to send the request header
	readHeaderTimeout = 10 * time.Second

	// writeTimeout is the time we wait for from the request being processed to the response being sent back to the client.
	// This can be long for inference and streaming.
	writeTimeout = 30 * time.Minute
	// idleTimeout is the time we wait for the http client to send the next request.
	// If the client doesn't send a request within this time, the connection is closed.
	idleTimeout = 120 * time.Second
	// maxHeaderBytes is the maximum size of the HTTP header. This is 1MB by default.
	// This is used to prevent HTTP request smuggling attacks.
	maxHeaderBytes = 1 << 20
)

// AgenticServer is the HTTP API server for the daemon
type AgenticServer struct {
	layer      *serving.AgenticLayer
	httpServer *http.Server
	mux        *http.ServeMux
}

// ServerOptions configures the API server.
type ServerOptions struct {
	// RateLimitRPS limits requests per second for execute and backends. 0 = disabled.
	RateLimitRPS int
}

// NewAgenticServer creates a new daemon API server.
func NewAgenticServer(layer *serving.AgenticLayer, listenAddress string, opts *ServerOptions) *AgenticServer {
	if opts == nil {
		opts = &ServerOptions{}
	}
	mux := http.NewServeMux()
	server := &AgenticServer{
		layer: layer,
		mux:   mux,
		httpServer: &http.Server{
			Addr:              listenAddress,
			Handler:           recoveryMiddleware(mux),
			ReadTimeout:       readTimeout,
			ReadHeaderTimeout: readHeaderTimeout,
			WriteTimeout:      writeTimeout, // long-running inference and streaming
			IdleTimeout:       idleTimeout,
			MaxHeaderBytes:    maxHeaderBytes, // 1MB
		},
	}

	server.registerRoutes(opts.RateLimitRPS)
	return server
}

// recoveryMiddleware catches panics in handlers, logs the stack, and returns 500.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("handler panic", "panic", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *AgenticServer) registerRoutes(rateLimitRPS int) {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.Handle("GET /metrics", promhttp.Handler())

	var executeHandler, chatHandler, backendsHandler http.Handler
	executeHandler = http.HandlerFunc(s.handleExecute)
	chatHandler = http.HandlerFunc(s.handleChatCompletions)
	backendsHandler = http.HandlerFunc(s.handleRegisterBackend)
	if rateLimitRPS > 0 {
		limiter := rate.NewLimiter(rate.Limit(rateLimitRPS), rateLimitRPS) // burst = RPS for predictable limiting
		executeHandler = rateLimitMiddleware(limiter, executeHandler)
		chatHandler = rateLimitMiddleware(limiter, chatHandler)
		backendsHandler = rateLimitMiddleware(limiter, backendsHandler)
	}
	s.mux.Handle("POST /api/v1/execute", executeHandler)
	s.mux.Handle("POST /v1/chat/completions", chatHandler)
	s.mux.Handle("POST /api/v1/backends", backendsHandler)

	s.mux.HandleFunc("GET /api/v1/backends", s.handleListBackends)
	s.mux.HandleFunc("PATCH /api/v1/backends/{name}", s.handleUpdateBackend)
	s.mux.HandleFunc("DELETE /api/v1/backends/{name}", s.handleDeleteBackend)
	s.mux.HandleFunc("POST /api/v1/workflows", s.handleRegisterWorkflow)
	s.mux.HandleFunc("POST /api/v1/workflow/complete", s.handleWorkflowComplete)

	s.mux.HandleFunc("POST /api/v1/policies", s.handleAddPolicy)
	s.mux.HandleFunc("GET /api/v1/policies", s.handleListPolicies)
	s.mux.HandleFunc("DELETE /api/v1/policies/{name}", s.handleRemovePolicy)

	s.mux.HandleFunc("PUT /api/v1/stages/{id}", s.handleUpsertStage)
	s.mux.HandleFunc("PATCH /api/v1/stages/{id}", s.handlePatchStage)
	s.mux.HandleFunc("GET /api/v1/stages/{id}", s.handleGetStage)
	s.mux.HandleFunc("GET /api/v1/stages", s.handleListStages)
	s.mux.HandleFunc("DELETE /api/v1/stages/{id}", s.handleDeleteStage)

	s.mux.HandleFunc("POST /api/v1/access/check", s.handleAccessCheck)
}

// rateLimitMiddleware returns 429 when the limiter rejects the request.
func rateLimitMiddleware(limiter *rate.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start starts the HTTP server
func (s *AgenticServer) Start() error {
	slog.Info("Starting daemon API server",
		"address", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *AgenticServer) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *AgenticServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]string{
		"status": "healthy",
	})
}

// ExecuteRequest is the request body for the execute endpoint.
// Inference options (stream, max_tokens, temperature, top_p) are embedded so the JSON body stays flat.
type ExecuteRequest struct {
	Backend  string          `json:"backend"`
	StageID  string          `json:"stage_id,omitempty"`
	Prompt   string          `json:"prompt,omitempty"`
	Messages []model.Message `json:"messages,omitempty"`
	Tools    []*model.Tool   `json:"tools,omitempty"`
	model.InferenceOptions

	WorkflowID  string            `json:"workflow_id,omitempty"`
	CachePolicy string            `json:"cache_policy,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	DataLabels  []string          `json:"data_labels,omitempty"`
}

// ExecuteResponse is the response body for the execute endpoint.
type ExecuteResponse struct {
	Success  bool            `json:"success"`
	Response *model.Response `json:"response,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func (s *AgenticServer) handleExecute(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req ExecuteRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Backend == "" {
		http.Error(w, "backend is required", http.StatusBadRequest)
		return
	}

	// Compute effective data labels: explicit labels merged with labels
	// inherited from upstream stages in the registered workflow DAG.
	effectiveLabels := req.DataLabels
	if req.WorkflowID != "" {
		var err error
		effectiveLabels, err = s.layer.WorkflowManager.EffectiveLabels(req.WorkflowID, req.StageID, req.DataLabels)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Access control enforcement (backend + tool + data).
	if len(req.Tags) > 0 {
		toolNames := make([]string, len(req.Tools))
		for i, t := range req.Tools {
			toolNames[i] = t.Name
		}
		d := s.layer.ValidateAccess(req.Tags, req.Backend, toolNames, effectiveLabels)
		if !d.Allowed {
			http.Error(w, d.Reason, http.StatusForbidden)
			return
		}
	}

	messages := req.Messages
	if req.Prompt != "" {
		messages = append(messages, model.Message{
			Role:    model.MessageRoleUser,
			Content: req.Prompt,
		})
	}

	ctx := r.Context()
	opts := req.InferenceOptions
	switch opts.GetSchedulingPolicy() {
	case model.SchedulingPolicyFCFS, model.SchedulingPolicyPriority:
		// supported
	default:
		http.Error(w, fmt.Sprintf("unsupported scheduling policy %q", opts.SchedulingPolicy), http.StatusBadRequest)
		return
	}
	switch opts.RequestSchedulingPolicy {
	case "", model.RequestSchedulingPolicyFCFS, model.RequestSchedulingPolicyPriority:
		// supported
	default:
		http.Error(w, fmt.Sprintf("unsupported request scheduling policy %q", opts.RequestSchedulingPolicy), http.StatusBadRequest)
		return
	}

	stageID := req.StageID
	chatOpts := serving.ChatOptions{
		WorkflowID:  req.WorkflowID,
		CachePolicy: req.CachePolicy,
	}

	if req.Stream {
		s.handleExecuteStream(w, ctx, req.Backend, stageID, messages, req.Tools, opts, chatOpts)
		return
	}

	response, err := s.layer.Execute(ctx, req.Backend, stageID, messages, req.Tools, opts, chatOpts)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		core.WriteJSONResponse(w, ExecuteResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	slog.Debug("Executed inference via API",
		"backend", req.Backend,
		"response_length", len(response.Content))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, ExecuteResponse{
		Success:  true,
		Response: response,
	})
}

func (s *AgenticServer) handleExecuteStream(w http.ResponseWriter, ctx context.Context, backend, stage string, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions, chatOpts serving.ChatOptions) {
	response, eventCh, err := s.layer.ExecuteStream(ctx, backend, stage, messages, tools, opts, chatOpts)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		core.WriteJSONResponse(w, ExecuteResponse{Success: false, Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("flusher not supported", "backend", backend)
		http.Error(w, "flusher not supported", http.StatusInternalServerError)
		return
	}

	if eventCh != nil {
		for ev := range eventCh {
			switch e := ev.(type) {
			case *model.ContentEvent:
				core.WriteSSEResponse(w, flusher, sseEventContent, map[string]string{"content": e.Content})
			case *model.ThinkingEvent:
				core.WriteSSEResponse(w, flusher, sseEventThinking, map[string]string{"thinking": e.Content})
			case *model.ToolCallEvent:
				core.WriteSSEResponse(w, flusher, "tool_call", map[string]any{"name": e.Name, "arguments": e.Arguments})
			}
		}
	}

	core.WriteSSEResponse(w, flusher, sseEventDone, ExecuteResponse{
		Success:  true,
		Response: response,
	})
}

// CostModelRequest is the JSON shape for per-backend token pricing.
type CostModelRequest struct {
	InputCostPerMToken  float64 `json:"input_cost_per_mtoken"`
	OutputCostPerMToken float64 `json:"output_cost_per_mtoken"`
}

// RegisterBackendRequest is the request body for registering an LLM backend.
// The provider implementation is selected by the model_id prefix (e.g. "openai:",
// "sglang:"); see internal/model for the registered factories.
type RegisterBackendRequest struct {
	Name           string            `json:"name"`                      // backend name (used as "model" in chat completion requests)
	Endpoint       string            `json:"endpoint"`                  // e.g. "http://vllm:8000/v1", "http://localhost:11434/v1"
	ModelID        string            `json:"model_id"`                  // provider-prefixed model identifier, e.g. "openai:gpt-4o", "sglang:Qwen/Qwen3-4B"
	APIKeyEnvVar   string            `json:"api_key_env_var,omitempty"` // optional env var name holding the API key
	MaxConcurrency *int              `json:"max_concurrency,omitempty"` // max concurrent requests (nil = default 1)
	QueueCapacity  *int              `json:"queue_capacity,omitempty"`  // max queued requests (nil = default)
	CostModel      *CostModelRequest `json:"cost_model,omitempty"`      // optional token pricing
	Quality        *float64          `json:"quality,omitempty"`         // platform-engineer-supplied capability prior in [0.0, 1.0]
}

// RegisterBackendResponse is the response body for register backend.
type RegisterBackendResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func (s *AgenticServer) handleRegisterBackend(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req RegisterBackendRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.Endpoint == "" {
		http.Error(w, "endpoint is required", http.StatusBadRequest)
		return
	}
	if req.ModelID == "" {
		http.Error(w, "model_id is required", http.StatusBadRequest)
		return
	}

	if req.Quality != nil {
		if q := *req.Quality; !(q >= 0 && q <= 1) {
			http.Error(w, fmt.Sprintf("quality must be in [0.0, 1.0]; got %v", q), http.StatusBadRequest)
			return
		}
	}
	if req.CostModel != nil {
		in, out := req.CostModel.InputCostPerMToken, req.CostModel.OutputCostPerMToken
		if !core.IsFinite(in) || !core.IsFinite(out) || in < 0 || out < 0 {
			http.Error(w, "cost_model rates must be finite non-negative numbers", http.StatusBadRequest)
			return
		}
	}

	backend := &core.LLMBackend{
		Endpoint:       req.Endpoint,
		APIKeyEnvVar:   req.APIKeyEnvVar,
		MaxConcurrency: req.MaxConcurrency,
		QueueCapacity:  req.QueueCapacity,
		Quality:        req.Quality,
	}
	if req.CostModel != nil {
		backend.CostModel = &core.CostModel{
			InputCostPerMToken:  req.CostModel.InputCostPerMToken,
			OutputCostPerMToken: req.CostModel.OutputCostPerMToken,
		}
	}
	if err := s.layer.AddLLMBackend(req.Name, backend, req.ModelID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("Registered LLM backend",
		"name", req.Name,
		"endpoint", req.Endpoint,
		"model_id", req.ModelID,
		"max_concurrency", backend.EffectiveMaxConcurrency(),
		"queue_capacity", backend.EffectiveQueueCapacity(),
		"quality", backend.Quality)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, RegisterBackendResponse{Success: true})
}

func (s *AgenticServer) handleUpdateBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "backend name is required in URL path", http.StatusBadRequest)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var update serving.BackendUpdate
	if err := json.NewDecoder(body).Decode(&update); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	if update.Quality != nil {
		if q := *update.Quality; !(q >= 0 && q <= 1) {
			http.Error(w, fmt.Sprintf("quality must be in [0.0, 1.0]; got %v", q), http.StatusBadRequest)
			return
		}
	}
	if update.CostModel != nil {
		in, out := update.CostModel.InputCostPerMToken, update.CostModel.OutputCostPerMToken
		if !core.IsFinite(in) || !core.IsFinite(out) || in < 0 || out < 0 {
			http.Error(w, "cost_model rates must be finite non-negative numbers", http.StatusBadRequest)
			return
		}
	}
	if update.MaxConcurrency != nil && *update.MaxConcurrency < 1 {
		http.Error(w, "max_concurrency must be >= 1", http.StatusBadRequest)
		return
	}

	if err := s.layer.UpdateBackend(name, update); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	slog.Info("Updated backend",
		"name", name,
		"cost_model_changed", update.CostModel != nil,
		"quality_changed", update.Quality != nil,
		"max_concurrency_changed", update.MaxConcurrency != nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]bool{"success": true})
}

// ListBackendsResponse is the response body for list backends.
type ListBackendsResponse struct {
	Backends []string `json:"backends"`
}

func (s *AgenticServer) handleListBackends(w http.ResponseWriter, r *http.Request) {
	names := s.layer.ListLLMBackends()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, ListBackendsResponse{Backends: names})
}

func (s *AgenticServer) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "backend name is required in URL path", http.StatusBadRequest)
		return
	}
	if err := s.layer.RemoveLLMBackend(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("Removed LLM backend", "name", name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]bool{"success": true})
}

// WorkflowCompleteRequest is the request body for the workflow/complete endpoint.
type WorkflowCompleteRequest struct {
	WorkflowID string   `json:"workflow_id"`
	Backends   []string `json:"backends"`
}

// WorkflowCompleteResponse is the response body for the workflow/complete endpoint.
type WorkflowCompleteResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func (s *AgenticServer) handleWorkflowComplete(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req WorkflowCompleteRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	if req.WorkflowID == "" {
		http.Error(w, "workflow_id is required", http.StatusBadRequest)
		return
	}

	s.layer.NotifyWorkflowComplete(r.Context(), req.WorkflowID, req.Backends)

	slog.Debug("Workflow complete notification",
		"workflow_id", req.WorkflowID,
		"backends", req.Backends)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, WorkflowCompleteResponse{Success: true})
}

// ---- Access control policy endpoints ----

// AddPolicyRequest is the request body for creating an access control policy.
type AddPolicyRequest struct {
	Name      string   `json:"name"`
	Subjects  []string `json:"subjects"`
	Resources []string `json:"resources"`
	Action    string   `json:"action"`
}

func (s *AgenticServer) handleAddPolicy(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req AddPolicyRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	p := &access.Policy{
		Name:      req.Name,
		Subjects:  req.Subjects,
		Resources: req.Resources,
		Action:    access.Action(req.Action),
	}
	if err := s.layer.PolicyStore.Add(p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	slog.Info("Added access control policy",
		"name", p.Name,
		"action", string(p.Action),
		"subjects", p.Subjects,
		"resources", p.Resources)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]bool{"success": true})
}

// ListPoliciesResponse is the response body for listing policies.
type ListPoliciesResponse struct {
	Policies []*access.Policy `json:"policies"`
}

func (s *AgenticServer) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	policies := s.layer.PolicyStore.List()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, ListPoliciesResponse{Policies: policies})
}

func (s *AgenticServer) handleRemovePolicy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "policy name is required in URL path", http.StatusBadRequest)
		return
	}
	if err := s.layer.PolicyStore.Remove(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	slog.Info("Removed access control policy", "name", name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]bool{"success": true})
}

// Workflow DAG registration for data label propagation decisions.

// RegisterWorkflowRequest is the request body for registering a workflow DAG.
type RegisterWorkflowRequest struct {
	WorkflowID        string                          `json:"workflow_id"`
	Edges             []WorkflowEdgeEntry             `json:"edges"`
	Declassifications []WorkflowDeclassificationEntry `json:"declassifications,omitempty"`
}

// WorkflowEdgeEntry is a single directed edge in the workflow DAG.
type WorkflowEdgeEntry struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// WorkflowDeclassificationEntry declares which labels a stage strips from propagation.
type WorkflowDeclassificationEntry struct {
	StageID string   `json:"stage_id"`
	Labels  []string `json:"labels"`
}

func (s *AgenticServer) handleRegisterWorkflow(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req RegisterWorkflowRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	if req.WorkflowID == "" {
		http.Error(w, "workflow_id is required", http.StatusBadRequest)
		return
	}

	edges := make([][2]string, len(req.Edges))
	for i, e := range req.Edges {
		if e.From == "" || e.To == "" {
			http.Error(w, fmt.Sprintf("edge %d: both from and to are required", i), http.StatusBadRequest)
			return
		}
		edges[i] = [2]string{e.From, e.To}
	}

	s.layer.WorkflowManager.RegisterEdges(req.WorkflowID, edges)

	if len(req.Declassifications) > 0 {
		declass := make(map[string][]string, len(req.Declassifications))
		for _, d := range req.Declassifications {
			if d.StageID == "" || len(d.Labels) == 0 {
				continue
			}
			declass[d.StageID] = d.Labels
		}
		if len(declass) > 0 {
			s.layer.WorkflowManager.RegisterDeclassifications(req.WorkflowID, declass)
		}
	}

	slog.Debug("Registered workflow DAG",
		"workflow_id", req.WorkflowID,
		"edges", len(edges),
		"declassifications", len(req.Declassifications))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]bool{"success": true})
}

// AccessCheckRequest is the request body for the access validation endpoint.
// Agents that manage their own inference can use this to check policies without executing.
type AccessCheckRequest struct {
	Tags       map[string]string `json:"tags"`
	Backend    string            `json:"backend,omitempty"`
	Tools      []string          `json:"tools,omitempty"`
	DataLabels []string          `json:"data_labels,omitempty"`
}

// AccessCheckResponse is the response body for the access validation endpoint.
type AccessCheckResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func (s *AgenticServer) handleAccessCheck(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req AccessCheckRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Tags) == 0 {
		http.Error(w, "tags are required", http.StatusBadRequest)
		return
	}

	d := s.layer.ValidateAccess(req.Tags, req.Backend, req.Tools, req.DataLabels)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, AccessCheckResponse{Allowed: d.Allowed, Reason: d.Reason})
}
