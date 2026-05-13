package serving

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving/memory"
	"go.uber.org/zap"
)

type backendEntry struct {
	backend        *core.LLMBackend
	modelID        string
	maxConcurrency int
	queueCapacity  int
}

// LLMBackendManager manages a pool of LLM backend configurations and their providers
type LLMBackendManager struct {
	backends      map[string]*backendEntry
	providers     map[string]model.Provider
	executors     map[string]*backendExecutor
	memoryManager *memory.DefaultManager
	mu            sync.RWMutex
}

// NewLLMBackendManager creates a new LLM backend manager.
func NewLLMBackendManager(mm *memory.DefaultManager) *LLMBackendManager {
	return &LLMBackendManager{
		backends:      make(map[string]*backendEntry),
		providers:     make(map[string]model.Provider),
		executors:     make(map[string]*backendExecutor),
		memoryManager: mm,
	}
}

// AddLLMBackend registers an LLM backend by name.
func (m *LLMBackendManager) AddLLMBackend(name string, backend *core.LLMBackend, modelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = &backendEntry{
		backend:        backend,
		modelID:        modelID,
		maxConcurrency: backend.EffectiveMaxConcurrency(),
		queueCapacity:  backend.EffectiveQueueCapacity(),
	}
	delete(m.providers, name)
	if exec, ok := m.executors[name]; ok {
		exec.close()
		delete(m.executors, name)
	}

	if m.memoryManager != nil && backend.Type == core.LLMInferenceAPITypeSGLang {
		baseURL := strings.TrimSuffix(strings.TrimRight(backend.Endpoint, "/"), "/v1")
		cc := memory.NewSGLangCacheController(baseURL)
		m.memoryManager.RegisterCacheController(name, cc)
		zap.L().Debug("Registered SGLang cache controller for backend", zap.String("backend", name))
	}
}

// BackendUpdate holds the optional fields that can be live-updated on a registered backend.
// Nil fields are left unchanged.
type BackendUpdate struct {
	CostModel      *core.CostModel `json:"cost_model,omitempty"`
	Quality        *float64        `json:"quality,omitempty"`
	MaxConcurrency *int            `json:"max_concurrency,omitempty"`
}

// UpdateBackend applies a partial update to an existing backend's mutable fields.
// Returns an error if the backend is not registered.
func (m *LLMBackendManager) UpdateBackend(name string, update BackendUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.backends[name]
	if !ok {
		return fmt.Errorf("backend %q not found", name)
	}
	if update.CostModel != nil {
		entry.backend.CostModel = update.CostModel
	}
	if update.Quality != nil {
		entry.backend.Quality = update.Quality
	}
	if update.MaxConcurrency != nil {
		entry.backend.MaxConcurrency = update.MaxConcurrency
		entry.maxConcurrency = entry.backend.EffectiveMaxConcurrency()
	}
	return nil
}

// GetModelID returns the modelID string for a registered backend, or "" if not found.
func (m *LLMBackendManager) GetModelID(backendName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.backends[backendName]; ok {
		return entry.modelID
	}
	return ""
}

// GetCostModel returns the CostModel for a registered backend, or nil if not found or unset.
func (m *LLMBackendManager) GetCostModel(backendName string) *core.CostModel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.backends[backendName]; ok {
		return entry.backend.CostModel
	}
	return nil
}

// GetModelProvider returns a cached provider for an LLM backend, creating it if necessary
func (m *LLMBackendManager) GetModelProvider(ctx context.Context, backendName string) (model.Provider, error) {
	m.mu.RLock()
	if provider, exists := m.providers[backendName]; exists {
		m.mu.RUnlock()
		return provider, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if provider, exists := m.providers[backendName]; exists {
		return provider, nil
	}

	entry, exists := m.backends[backendName]
	if !exists {
		return nil, fmt.Errorf("llm_backend '%s' not found", backendName)
	}

	provider, err := model.NewProviderFromBackend(entry.backend, entry.modelID)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider for llm_backend '%s': %w", backendName, err)
	}

	m.providers[backendName] = provider

	zap.L().Debug("Created and cached a model provider for LLM backend",
		zap.String("backend_name", backendName),
		zap.String("model", entry.modelID))

	return provider, nil
}

func (m *LLMBackendManager) getOrCreateExecutorLocked(backendName string) (*backendExecutor, error) {
	entry, exists := m.backends[backendName]
	if !exists {
		return nil, fmt.Errorf("llm_backend '%s' not found", backendName)
	}
	if exec, ok := m.executors[backendName]; ok {
		return exec, nil
	}
	exec := newBackendExecutor(backendName, m, entry.maxConcurrency, entry.queueCapacity, m.memoryManager)
	m.executors[backendName] = exec
	return exec, nil
}

// ChatOptions carries optional metadata for a scheduled chat request.
type ChatOptions struct {
	WorkflowID  string
	CachePolicy string
}

// ScheduleChat queues a request for execution under the backend's scheduling policy.
// stageName identifies the stage queue inside the backend. Empty uses "default".
func (m *LLMBackendManager) ScheduleChat(ctx context.Context, backendName, stageName string, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions, chatOpts ...ChatOptions) (*model.Response, <-chan model.StreamEvent, error) {
	m.mu.Lock()
	exec, err := m.getOrCreateExecutorLocked(backendName)
	m.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}

	req := &scheduledRequest{
		ctx:        ctx,
		backend:    backendName,
		stageName:  stageName,
		messages:   messages,
		tools:      tools,
		opts:       opts,
		enqueuedAt: time.Now(),
		resultCh:   make(chan scheduledResult, 1),
	}
	if len(chatOpts) > 0 {
		req.workflowID = chatOpts[0].WorkflowID
		req.cachePolicy = chatOpts[0].CachePolicy
	}
	if err := exec.enqueue(req); err != nil {
		return nil, nil, err
	}

	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case result := <-req.resultCh:
		return result.response, result.streamCh, result.err
	}
}

// HealthStatus represents the health status of an LLM backend
type HealthStatus string

const (
	HealthStatusHealthy     HealthStatus = "healthy"
	HealthStatusDegraded    HealthStatus = "degraded"
	HealthStatusUnavailable HealthStatus = "unavailable"
)

const (
	healthCheckTimeout           = 5 * time.Second
	healthCheckDegradedThreshold = 2 * time.Second
)

// GetHealthStatus returns the health status of an LLM backend
func (m *LLMBackendManager) GetHealthStatus(ctx context.Context, backendName string) (HealthStatus, error) {
	m.mu.RLock()
	_, exists := m.backends[backendName]
	m.mu.RUnlock()
	if !exists {
		return HealthStatusUnavailable, fmt.Errorf("llm_backend '%s' not found", backendName)
	}

	provider, err := m.GetModelProvider(ctx, backendName)
	if err != nil {
		return HealthStatusUnavailable, fmt.Errorf("failed to get provider: %w", err)
	}

	healthCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	start := time.Now()
	err = provider.EnsureReady(healthCtx)
	duration := time.Since(start)

	if healthCtx.Err() == context.DeadlineExceeded {
		return HealthStatusUnavailable, fmt.Errorf("health check timed out after %v", healthCheckTimeout)
	}

	if err != nil {
		return HealthStatusUnavailable, fmt.Errorf("health check failed: %w", err)
	}

	if duration > healthCheckDegradedThreshold {
		return HealthStatusDegraded, nil
	}

	return HealthStatusHealthy, nil
}

type backendCandidate struct {
	name string
	cm   *core.CostModel
}

// sortBackendCandidates sorts backend candidates by output cost, then input cost, then name.
// The intuition is that we want to pick the cheapest backend that is still good enough for the accuracy threshold.
// Typically, output costs are much higher than input costs, so we want to prioritize them.
// If all else is equal, we want to pick the backend with the lowest name lexicographically to have a deterministic result.
func sortBackendCandidates(c []backendCandidate) {
	sort.Slice(c, func(i, j int) bool {
		if c[i].cm.OutputCostPerMToken != c[j].cm.OutputCostPerMToken {
			return c[i].cm.OutputCostPerMToken < c[j].cm.OutputCostPerMToken
		}
		if c[i].cm.InputCostPerMToken != c[j].cm.InputCostPerMToken {
			return c[i].cm.InputCostPerMToken < c[j].cm.InputCostPerMToken
		}
		return c[i].name < c[j].name
	})
}

// SelectBackendByAccuracy returns the cheapest registered backend whose Quality >= accuracy
// and that has a CostModel set. Ties are broken by ascending output cost, then input cost,
// then backend name.
//
// The policy parameter controls fallback behavior when no backend meets the threshold:
//   - "strict": return an error.
//   - "prefer" (or empty, the default): fall back to the cheapest costed backend,
//     or defaultBackend if no backends have cost models.
func (m *LLMBackendManager) SelectBackendByAccuracy(accuracy float64, policy string, defaultBackend string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var qualified, costed []backendCandidate
	for name, entry := range m.backends {
		b := entry.backend
		if b.CostModel == nil {
			continue
		}
		costed = append(costed, backendCandidate{name: name, cm: b.CostModel})
		if b.Quality != nil && *b.Quality >= accuracy {
			qualified = append(qualified, backendCandidate{name: name, cm: b.CostModel})
		}
	}

	if len(qualified) > 0 {
		sortBackendCandidates(qualified)
		return qualified[0].name, nil
	}

	if policy == model.AccuracyPolicyStrict {
		return "", fmt.Errorf("no backend with quality >= %v and a cost model; registered backends: %s",
			accuracy, m.describeBackendsLocked())
	}

	// "prefer" fallback: cheapest backend with a cost model, regardless of quality.
	// If no backends have cost models, return the default backend.
	if len(costed) == 0 {
		return defaultBackend, nil
	}
	sortBackendCandidates(costed)
	return costed[0].name, nil
}

// describeBackendsLocked returns a human-readable summary of registered backends.
// Caller must hold at least m.mu.RLock().
func (m *LLMBackendManager) describeBackendsLocked() string {
	if len(m.backends) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(m.backends))
	for name, entry := range m.backends {
		hasCost := entry.backend.CostModel != nil
		qStr := "unscored"
		if entry.backend.Quality != nil {
			qStr = fmt.Sprintf("%.2f", *entry.backend.Quality)
		}
		parts = append(parts, fmt.Sprintf("%s(quality=%s, has_cost=%v)", name, qStr, hasCost))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// ListLLMBackends returns a list of all LLM backend names
func (m *LLMBackendManager) ListLLMBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backendNames := make([]string, 0, len(m.backends))
	for backendName := range m.backends {
		backendNames = append(backendNames, backendName)
	}
	return backendNames
}
