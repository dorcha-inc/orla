package serving

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving/memory"
	"log/slog"
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
	db            *sql.DB // nil disables persistence (tests)
	mu            sync.RWMutex
}

// NewLLMBackendManager creates a new LLM backend manager. db may be nil; when
// non-nil, backend records are persisted across daemon restarts.
func NewLLMBackendManager(mm *memory.DefaultManager, db *sql.DB) *LLMBackendManager {
	return &LLMBackendManager{
		backends:      make(map[string]*backendEntry),
		providers:     make(map[string]model.Provider),
		executors:     make(map[string]*backendExecutor),
		memoryManager: mm,
		db:            db,
	}
}

// LoadPersisted reads backend records from SQLite and rehydrates the in-memory
// runtime state. Providers are created lazily on first use. No-op when db is nil.
func (m *LLMBackendManager) LoadPersisted(ctx context.Context) error {
	if m.db == nil {
		return nil
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT name, endpoint, model_id, api_key_env_var,
			max_concurrency, queue_capacity,
			input_cost_per_mtoken, output_cost_per_mtoken, quality
		FROM backends`)
	if err != nil {
		return fmt.Errorf("load backends: %w", err)
	}
	defer core.LogDeferredError(rows.Close)

	m.mu.Lock()
	defer m.mu.Unlock()
	for rows.Next() {
		var (
			name, endpoint, modelID, apiKey string
			maxConc, qCap                   sql.NullInt64
			inCost, outCost, quality        sql.NullFloat64
		)
		if err := rows.Scan(&name, &endpoint, &modelID, &apiKey, &maxConc, &qCap, &inCost, &outCost, &quality); err != nil {
			return fmt.Errorf("scan backend row: %w", err)
		}
		b := &core.LLMBackend{Endpoint: endpoint, APIKeyEnvVar: apiKey}
		if maxConc.Valid {
			v := int(maxConc.Int64)
			b.MaxConcurrency = &v
		}
		if qCap.Valid {
			v := int(qCap.Int64)
			b.QueueCapacity = &v
		}
		if inCost.Valid && outCost.Valid {
			b.CostModel = &core.CostModel{InputCostPerMToken: inCost.Float64, OutputCostPerMToken: outCost.Float64}
		}
		if quality.Valid {
			v := quality.Float64
			b.Quality = &v
		}
		m.registerLocked(name, b, modelID)
	}
	return rows.Err()
}

// AddLLMBackend registers an LLM backend by name. Persists to the database
// when configured.
func (m *LLMBackendManager) AddLLMBackend(name string, backend *core.LLMBackend, modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db != nil {
		if err := m.persistLocked(name, backend, modelID); err != nil {
			return err
		}
	}
	m.registerLocked(name, backend, modelID)
	return nil
}

// registerLocked installs the in-memory runtime state for a backend.
// Caller must hold m.mu.
func (m *LLMBackendManager) registerLocked(name string, backend *core.LLMBackend, modelID string) {
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
	if m.memoryManager != nil && strings.HasPrefix(modelID, "sglang:") {
		baseURL := strings.TrimSuffix(strings.TrimRight(backend.Endpoint, "/"), "/v1")
		cc := memory.NewSGLangCacheController(baseURL)
		m.memoryManager.RegisterCacheController(name, cc)
		slog.Debug("Registered SGLang cache controller for backend", "backend", name)
	}
}

func (m *LLMBackendManager) persistLocked(name string, backend *core.LLMBackend, modelID string) error {
	now := time.Now().Unix()
	var maxConc, qCap any
	if backend.MaxConcurrency != nil {
		maxConc = int64(*backend.MaxConcurrency)
	}
	if backend.QueueCapacity != nil {
		qCap = int64(*backend.QueueCapacity)
	}
	var inCost, outCost any
	if backend.CostModel != nil {
		inCost = backend.CostModel.InputCostPerMToken
		outCost = backend.CostModel.OutputCostPerMToken
	}
	var quality any
	if backend.Quality != nil {
		quality = *backend.Quality
	}
	_, err := m.db.Exec(`
		INSERT INTO backends (
			name, endpoint, model_id, api_key_env_var,
			max_concurrency, queue_capacity,
			input_cost_per_mtoken, output_cost_per_mtoken, quality,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			endpoint=excluded.endpoint,
			model_id=excluded.model_id,
			api_key_env_var=excluded.api_key_env_var,
			max_concurrency=excluded.max_concurrency,
			queue_capacity=excluded.queue_capacity,
			input_cost_per_mtoken=excluded.input_cost_per_mtoken,
			output_cost_per_mtoken=excluded.output_cost_per_mtoken,
			quality=excluded.quality,
			updated_at=excluded.updated_at;
	`,
		name, backend.Endpoint, modelID, backend.APIKeyEnvVar,
		maxConc, qCap, inCost, outCost, quality,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("persist backend %q: %w", name, err)
	}
	return nil
}

// RemoveLLMBackend deletes a backend's record and tears down runtime state.
// No error if the backend wasn't registered.
func (m *LLMBackendManager) RemoveLLMBackend(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db != nil {
		if _, err := m.db.Exec(`DELETE FROM backends WHERE name = ?`, name); err != nil {
			return fmt.Errorf("delete backend %q: %w", name, err)
		}
	}
	delete(m.backends, name)
	delete(m.providers, name)
	if exec, ok := m.executors[name]; ok {
		exec.close()
		delete(m.executors, name)
	}
	return nil
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
	if m.db != nil {
		if err := m.persistLocked(name, entry.backend, entry.modelID); err != nil {
			return err
		}
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

	slog.Debug("Created and cached a model provider for LLM backend",
		"backend_name", backendName,
		"model", entry.modelID)

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
