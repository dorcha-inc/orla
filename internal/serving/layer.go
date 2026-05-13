// Package serving implements a minimal programmatic serving layer.
package serving

import (
	"context"
	"database/sql"
	"fmt"
	"maps"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving/access"
	"github.com/harvard-cns/orla/internal/serving/memory"
	"github.com/harvard-cns/orla/internal/stages"
	"log/slog"
)

// AgenticLayer is the serving layer that manages LLM backends and executes inference.
type AgenticLayer struct {
	llmBackendManager *LLMBackendManager
	MemoryManager     *memory.DefaultManager
	WorkflowManager   *core.WorkflowManager
	PolicyStore       *access.Store
	StageRegistry     *stages.Registry
	policyEvaluator   *access.Evaluator
}

// AgenticLayerOptions configures a layer. Both fields are optional; tests that
// don't exercise persistence or stages pass an empty struct.
type AgenticLayerOptions struct {
	StageRegistry *stages.Registry
	DB            *sql.DB
}

// NewAgenticLayer creates a new serving layer.
func NewAgenticLayer(opts AgenticLayerOptions) *AgenticLayer {
	wm := core.NewWorkflowManager()
	mm := memory.NewDefaultManager(memory.DefaultManagerConfig{}, wm)
	ps := access.NewStore()
	return &AgenticLayer{
		llmBackendManager: NewLLMBackendManager(mm, opts.DB),
		MemoryManager:     mm,
		WorkflowManager:   wm,
		PolicyStore:       ps,
		StageRegistry:     opts.StageRegistry,
		policyEvaluator:   access.NewEvaluator(ps),
	}
}

// AddLLMBackend registers an LLM backend by name. Returns an error only if
// persistence to the backing store fails.
func (l *AgenticLayer) AddLLMBackend(name string, backend *core.LLMBackend, modelID string) error {
	return l.llmBackendManager.AddLLMBackend(name, backend, modelID)
}

// RemoveLLMBackend removes a registered backend and tears down runtime state.
// Returns an error only if removal from the backing store fails.
func (l *AgenticLayer) RemoveLLMBackend(name string) error {
	return l.llmBackendManager.RemoveLLMBackend(name)
}

// LoadPersistedBackends rehydrates backends from the database. No-op if no DB.
func (l *AgenticLayer) LoadPersistedBackends(ctx context.Context) error {
	return l.llmBackendManager.LoadPersisted(ctx)
}

// GetModelProvider returns the model provider for a named LLM backend.
func (l *AgenticLayer) GetModelProvider(ctx context.Context, backendName string) (model.Provider, error) {
	return l.llmBackendManager.GetModelProvider(ctx, backendName)
}

// Execute runs a single non-streaming inference call against the named LLM backend.
// For streaming, use ExecuteStream instead. opts.Stream must be false.
func (l *AgenticLayer) Execute(ctx context.Context, serverName, stageName string, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions, chatOpts ...ChatOptions) (*model.Response, error) {
	if opts.Stream {
		return nil, fmt.Errorf("Execute does not support streaming, use ExecuteStream instead")
	}

	response, _, err := l.llmBackendManager.ScheduleChat(ctx, serverName, stageName, messages, tools, opts, chatOpts...)
	if err != nil {
		return nil, fmt.Errorf("inference failed on server '%s': %w", serverName, err)
	}
	slog.Debug("Executed inference",
		"server", serverName,
		"response_length", len(response.Content))
	return response, nil
}

// ExecuteStream runs inference with streaming. It returns the response (filled as the stream
// is consumed), a channel of stream events, and an error. The caller must consume the channel
// until closed; the response content, tool_calls, and metrics are populated by the provider's
// goroutine as the stream completes. opts.Stream must be true.
func (l *AgenticLayer) ExecuteStream(ctx context.Context, serverName, stageName string, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions, chatOpts ...ChatOptions) (*model.Response, <-chan model.StreamEvent, error) {
	if !opts.Stream {
		return nil, nil, fmt.Errorf("ExecuteStream requires opts.Stream to be true")
	}

	response, ch, err := l.llmBackendManager.ScheduleChat(ctx, serverName, stageName, messages, tools, opts, chatOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("inference failed on server '%s': %w", serverName, err)
	}
	return response, ch, nil
}

// GetLLMBackendHealth returns the health status of a named LLM backend.
func (l *AgenticLayer) GetLLMBackendHealth(ctx context.Context, serverName string) (HealthStatus, error) {
	return l.llmBackendManager.GetHealthStatus(ctx, serverName)
}

// ListLLMBackends returns all registered LLM backend names.
func (l *AgenticLayer) ListLLMBackends() []string {
	return l.llmBackendManager.ListLLMBackends()
}

// UpdateBackend applies a partial update to a registered backend.
func (l *AgenticLayer) UpdateBackend(name string, update BackendUpdate) error {
	return l.llmBackendManager.UpdateBackend(name, update)
}

// GetCostModel returns the CostModel for a registered backend, or nil if not found or unset.
func (l *AgenticLayer) GetCostModel(backendName string) *core.CostModel {
	return l.llmBackendManager.GetCostModel(backendName)
}

// NotifyWorkflowComplete emits TransitionWorkflowComplete signals for each
// backend the workflow used, then deregisters the workflow from the tracker.
func (l *AgenticLayer) NotifyWorkflowComplete(ctx context.Context, workflowID string, backends []string) {
	if l.MemoryManager == nil {
		return
	}
	for _, backend := range backends {
		l.MemoryManager.OnTransition(ctx, memory.StageTransition{
			TransitionType: memory.TransitionWorkflowComplete,
			WorkflowID:     workflowID,
			Backend:        backend,
		})
	}
	l.WorkflowManager.Deregister(workflowID)
}

// CheckBackendAccess checks whether the given tags permit access to the named backend.
func (l *AgenticLayer) CheckBackendAccess(tags map[string]string, backendName string) access.Decision {
	return l.policyEvaluator.CheckAccess(tags, access.ResourceTypeBackend, backendName)
}

// CheckToolAccess checks whether the given tags permit all requested tools.
// Returns the first denial encountered, or an allowed decision if all tools pass.
func (l *AgenticLayer) CheckToolAccess(tags map[string]string, tools []*model.Tool) access.Decision {
	for _, t := range tools {
		if d := l.policyEvaluator.CheckAccess(tags, access.ResourceTypeTool, t.Name); !d.Allowed {
			return d
		}
	}
	return access.Decision{Allowed: true}
}

// CheckDataAccess checks whether data with the given labels may be sent to the named backend.
func (l *AgenticLayer) CheckDataAccess(tags map[string]string, backendName string, dataLabels []string) access.Decision {
	for _, label := range dataLabels {
		// For data policies, the subject is the backend receiving the data,
		// and the resource is the data label.
		backendTags := map[string]string{"backend": backendName}
		// Merge request tags so policies can match on either.
		maps.Copy(backendTags, tags)

		if d := l.policyEvaluator.CheckAccess(backendTags, access.ResourceTypeData, label); !d.Allowed {
			return d
		}
	}
	return access.Decision{Allowed: true}
}

// CheckToolAccessByName checks whether the given tags permit a single tool by name.
func (l *AgenticLayer) CheckToolAccessByName(tags map[string]string, toolName string) access.Decision {
	return l.policyEvaluator.CheckAccess(tags, access.ResourceTypeTool, toolName)
}

// ValidateAccess runs all access control checks for a request and returns the
// first denial, or an allowed decision if everything passes. Both handleExecute
// and handleAccessCheck call this method.
func (l *AgenticLayer) ValidateAccess(
	tags map[string]string,
	backend string,
	toolNames []string,
	dataLabels []string,
) access.Decision {
	// Backend check.
	if backend != "" {
		if d := l.CheckBackendAccess(tags, backend); !d.Allowed {
			return access.Decision{Allowed: false, Reason: fmt.Sprintf("access denied to backend %q: %s", backend, d.Reason)}
		}
	}

	// Tool checks.
	for _, tool := range toolNames {
		if d := l.CheckToolAccessByName(tags, tool); !d.Allowed {
			return access.Decision{Allowed: false, Reason: fmt.Sprintf("tool access denied: %s", d.Reason)}
		}
	}

	// Data label checks.
	if backend != "" && len(dataLabels) > 0 {
		if d := l.CheckDataAccess(tags, backend, dataLabels); !d.Allowed {
			return access.Decision{Allowed: false, Reason: fmt.Sprintf("data access denied for backend %q: %s", backend, d.Reason)}
		}
	}

	return access.Decision{Allowed: true}
}

// StartPressureMonitor launches the background memory pressure polling loop.
// It dynamically queries the current set of backends on each tick and stops
// when ctx is cancelled.
func (l *AgenticLayer) StartPressureMonitor(ctx context.Context) {
	go l.MemoryManager.StartPressureMonitor(ctx, l.llmBackendManager.ListLLMBackends, 0)
}
