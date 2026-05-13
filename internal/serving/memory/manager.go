package memory

import (
	"context"
	"sync"
	"time"

	"github.com/harvard-cns/orla/internal/core"
	"log/slog"
)

// CacheController is an optional interface that LLM providers can implement
// to support explicit cache management. Providers that don't support it
// simply don't implement the interface, and the Memory Manager falls back
// to soft-flush (mark as stale and let LRU reclaim).
type CacheController interface {
	FlushPrefix(ctx context.Context, sessionID string) error
	MemoryUsage(ctx context.Context) (*MemoryStats, error)
}

// MemoryStats reports a backend's memory utilization.
type MemoryStats struct {
	UsedBytes  int64
	TotalBytes int64
	Pressure   float64 // 0.0 to 1.0
}

// Manager is the Memory Manager interface. It receives stage transition
// signals from the Workflow Executor and returns cache management actions.
type Manager interface {
	OnTransition(ctx context.Context, signal StageTransition) CacheAction
	OnMemoryPressure(ctx context.Context, backend string, pressure float64) []CacheAction
	RegisterWorkflow(workflowID string)
	DeregisterWorkflow(workflowID string)
	RecordInflight(req core.InflightRequest)
	ClearInflight(backend, requestID string)
}

// DefaultManagerConfig configures the DefaultManager.
type DefaultManagerConfig struct {
	PreserveThreshold *int
	PressureThreshold *float64
}

const defaultPressureThreshold = 0.85

// EffectivePreserveThreshold returns the configured threshold or defaultPreserveThresholdTokens (256).
func (c DefaultManagerConfig) EffectivePreserveThreshold() int {
	if c.PreserveThreshold == nil || *c.PreserveThreshold <= 0 {
		return defaultPreserveThresholdTokens
	}
	return *c.PreserveThreshold
}

// EffectivePressureThreshold returns the configured threshold or 0.85.
func (c DefaultManagerConfig) EffectivePressureThreshold() float64 {
	if c.PressureThreshold == nil || *c.PressureThreshold <= 0 {
		return defaultPressureThreshold
	}
	return *c.PressureThreshold
}

// DefaultManager is the default Memory Manager implementation. It composes
// the three paper policies (preserve on small increment, flush at boundary,
// flush under pressure) with workflow state tracking and in-flight awareness.
type DefaultManager struct {
	tracker          *Tracker
	chain            *PolicyChain
	pressurePolicy   *FlushUnderPressurePolicy
	cacheControllers map[string]CacheController
	mu               sync.RWMutex
}

// NewDefaultManager creates a new DefaultManager with the standard policy chain.
func NewDefaultManager(cfg DefaultManagerConfig, wm *core.WorkflowManager) *DefaultManager {
	pressurePolicy := NewFlushUnderPressurePolicy(cfg.EffectivePressureThreshold())
	chain := NewPolicyChain(
		NewPreserveOnSmallIncrementPolicy(cfg.EffectivePreserveThreshold()),
		NewFlushAtBoundaryPolicy(),
	)
	return &DefaultManager{
		tracker:          NewTracker(wm),
		chain:            chain,
		pressurePolicy:   pressurePolicy,
		cacheControllers: make(map[string]CacheController),
	}
}

// RegisterCacheController associates a CacheController with a backend name.
func (m *DefaultManager) RegisterCacheController(backend string, cc CacheController) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cacheControllers[backend] = cc
}

func (m *DefaultManager) getCacheController(backend string) CacheController {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cacheControllers[backend]
}

// RegisterWorkflow initializes tracking for a new workflow execution.
func (m *DefaultManager) RegisterWorkflow(workflowID string) {
	m.tracker.RegisterWorkflow(workflowID)
	slog.Debug("Memory manager: registered workflow", "workflow_id", workflowID)
}

// DeregisterWorkflow removes a workflow from tracking.
func (m *DefaultManager) DeregisterWorkflow(workflowID string) {
	m.tracker.DeregisterWorkflow(workflowID)
	slog.Debug("Memory manager: deregistered workflow", "workflow_id", workflowID)
}

// RecordInflight marks a request as in-flight.
func (m *DefaultManager) RecordInflight(req core.InflightRequest) {
	m.tracker.RecordInflight(req)
}

// ClearInflight removes an in-flight request.
func (m *DefaultManager) ClearInflight(backend, requestID string) {
	m.tracker.ClearInflight(backend, requestID)
}

// OnTransition processes a stage lifecycle signal and returns a cache action.
//
// Resolution order:
//  1. Stage-level explicit CachePolicy override ("preserve" or "flush")
//  2. Policy chain (preserve on small increment -> flush at boundary)
//  3. Noop (let the backend's default LRU handle it)
func (m *DefaultManager) OnTransition(ctx context.Context, signal StageTransition) CacheAction {
	// Update tracker state.
	switch signal.TransitionType {
	case TransitionStageStart:
		m.tracker.OnStageStart(signal)
	case TransitionStageComplete:
		m.tracker.OnStageComplete(signal)
	case TransitionWorkflowComplete:
		m.tracker.OnStageComplete(signal)
	}

	// Stage-level explicit override takes precedence.
	if signal.CachePolicy == "preserve" {
		action := CacheAction{
			Type:       CacheActionPreserve,
			WorkflowID: signal.WorkflowID,
			Backend:    signal.Backend,
			Reason:     "stage-level override: preserve",
		}
		m.logAction(signal, action)
		return action
	}
	if signal.CachePolicy == "flush" {
		action := CacheAction{
			Type:       CacheActionFlush,
			WorkflowID: signal.WorkflowID,
			Backend:    signal.Backend,
			Reason:     "stage-level override: flush",
		}
		m.executeFlush(ctx, action)
		m.logAction(signal, action)
		return action
	}

	// Evaluate the policy chain.
	action := m.chain.Evaluate(signal, m.tracker)
	if action.Type == CacheActionFlush {
		m.executeFlush(ctx, action)
	}
	if action.Type == CacheActionPreserve || action.Type == CacheActionFlush {
		m.logAction(signal, action)
	}
	return action
}

// OnMemoryPressure evaluates whether to flush idle workflow caches under pressure.
func (m *DefaultManager) OnMemoryPressure(_ context.Context, backend string, pressure float64) []CacheAction {
	actions := m.pressurePolicy.EvaluatePressure(backend, pressure, m.tracker)
	for _, action := range actions {
		m.tracker.MarkBackendFlushed(action.WorkflowID, action.Backend)
		slog.Info("Memory manager: pressure flush",
			"workflow_id", action.WorkflowID,
			"backend", action.Backend,
			"pressure", pressure)
	}
	return actions
}

// executeFlush performs a soft flush: marks the cache as stale in the tracker
// and, if available, calls the backend's CacheController.
func (m *DefaultManager) executeFlush(ctx context.Context, action CacheAction) {
	m.tracker.MarkBackendFlushed(action.WorkflowID, action.Backend)
	cc := m.getCacheController(action.Backend)
	if cc == nil {
		return
	}
	// Only attempt a hard flush if no other workflow is in-flight on this backend.
	inflightWFs := m.tracker.InflightWorkflowsOnBackend(action.Backend)
	delete(inflightWFs, action.WorkflowID)
	if len(inflightWFs) > 0 {
		slog.Debug("Memory manager: skipping hard flush, other workflows in-flight",
			"backend", action.Backend,
			"other_inflight", len(inflightWFs))
		return
	}
	if err := cc.FlushPrefix(ctx, action.WorkflowID); err != nil {
		slog.Warn("Memory manager: flush failed",
			"backend", action.Backend,
			"error", err)
	}
}

func (m *DefaultManager) logAction(signal StageTransition, action CacheAction) {
	slog.Debug("Memory manager: cache action",
		"workflow_id", signal.WorkflowID,
		"stage_id", signal.StageID,
		"transition", string(signal.TransitionType),
		"action", string(action.Type),
		"reason", action.Reason)
}

// StartPressureMonitor launches a loop that periodically queries backends
// for memory pressure and triggers flush actions. It blocks until ctx is cancelled.
// backendsFn is called on each tick to discover the current set of backends,
// so dynamically registered backends are picked up automatically.
func (m *DefaultManager) StartPressureMonitor(ctx context.Context, backendsFn func() []string, interval time.Duration) {
	if interval <= 0 {
		slog.Warn("Memory manager: pressure monitor interval is less than or equal to 0, setting to 2 seconds")
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pollPressure(ctx, backendsFn())
		}
	}
}

func (m *DefaultManager) pollPressure(ctx context.Context, backends []string) {
	for _, backend := range backends {
		cc := m.getCacheController(backend)
		if cc == nil {
			continue
		}
		stats, err := cc.MemoryUsage(ctx)
		if err != nil {
			slog.Debug("Memory manager: failed to query memory usage",
				"backend", backend,
				"error", err)
			continue
		}
		if stats.Pressure >= m.pressurePolicy.PressureThreshold {
			m.OnMemoryPressure(ctx, backend, stats.Pressure)
		}
	}
}
