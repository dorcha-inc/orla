// Package serving implements the Agentic Serving Layer (RFC 5).
package serving

import (
	"context"
	"fmt"
	"sync"

	"github.com/dorcha-inc/orla/internal/config"
	"go.uber.org/zap"
)

// CachePolicyEvaluator evaluates cache policies based on various factors
type CachePolicyEvaluator struct {
	// policies maps server names to their cache policies
	policies map[string]*config.CacheConfig
}

// NewCachePolicyEvaluator creates a new cache policy evaluator
func NewCachePolicyEvaluator(servers []*config.LLMServerConfig) *CachePolicyEvaluator {
	policies := make(map[string]*config.CacheConfig)
	for _, server := range servers {
		if server.Cache != nil {
			policies[server.Name] = server.Cache
		}
	}

	return &CachePolicyEvaluator{
		policies: policies,
	}
}

// EvaluateDecision evaluates whether to flush or preserve cache based on the policy
// Returns true if cache should be flushed, false if it should be preserved
func (e *CachePolicyEvaluator) EvaluateDecision(serverName string, turnSize int, memoryPressure float64, isFinalIteration bool) (bool, error) {
	policy, exists := e.policies[serverName]
	if !exists {
		// No cache policy configured, default to preserve
		return false, nil
	}

	// First, check if flush_after_final modifier is set and this is the final iteration
	// This modifier can be applied to any policy
	if isFinalIteration && policy.FlushAfterFinal {
		return true, nil
	}

	// Then evaluate the specific policy logic
	switch policy.Policy {
	case config.CachePolicyPreserve:
		// Always preserve cache (unless flush_after_final modifier was set, which is already handled above)
		return false, nil

	case config.CachePolicyAggressiveFlush:
		// Always flush (regardless of other conditions)
		return true, nil

	case config.CachePolicyPreserveOnSmallTurns:
		// Flush if turn size exceeds threshold
		threshold := policy.SmallTurnThreshold
		if threshold <= 0 {
			threshold = 100 // Default threshold
		}
		return turnSize > threshold, nil

	case config.CachePolicyFlushUnderPressure:
		// Flush if memory pressure exceeds threshold
		threshold := policy.MemoryPressureThreshold
		if threshold <= 0 {
			threshold = 0.85 // Default threshold
		}
		return memoryPressure > threshold, nil

	default:
		return false, fmt.Errorf("unknown cache policy: %s", policy.Policy)
	}
}

// CacheManager tracks cache state and makes flush/preserve decisions
type CacheManager struct {
	evaluator *CachePolicyEvaluator
	// cacheState tracks the state of each server's cache
	cacheState map[string]*CacheState
	// mu protects access to cacheState
	mu sync.RWMutex
}

// CacheState represents the state of a cache for an LLM server
type CacheState struct {
	// ServerName is the name of the LLM server
	ServerName string
	// IsFlushed indicates whether the cache has been flushed
	IsFlushed bool
	// LastTurnSize is the size of the last turn (in tokens)
	LastTurnSize int
	// LastMemoryPressure is the last known memory pressure (0.0-1.0)
	LastMemoryPressure float64
}

// NewCacheManager creates a new cache manager
func NewCacheManager(evaluator *CachePolicyEvaluator) *CacheManager {
	return &CacheManager{
		evaluator:  evaluator,
		cacheState: make(map[string]*CacheState),
	}
}

// GetOrCreateCacheState gets or creates a cache state for an LLM server
// This method locks the mutex internally
func (cm *CacheManager) GetOrCreateCacheState(serverName string) *CacheState {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.getOrCreateCacheStateUnsafe(serverName)
}

// getOrCreateCacheStateUnsafe gets or creates a cache state without locking
// Caller must hold cm.mu lock
func (cm *CacheManager) getOrCreateCacheStateUnsafe(serverName string) *CacheState {
	if state, exists := cm.cacheState[serverName]; exists {
		return state
	}

	state := &CacheState{
		ServerName: serverName,
		IsFlushed:  false,
	}
	cm.cacheState[serverName] = state
	return state
}

// ShouldFlush determines if the cache should be flushed based on the policy
func (cm *CacheManager) ShouldFlush(ctx context.Context, serverName string, turnSize int, memoryPressure float64, isFinalIteration bool) (bool, error) {
	// Evaluate policy decision
	shouldFlush, err := cm.evaluator.EvaluateDecision(serverName, turnSize, memoryPressure, isFinalIteration)
	if err != nil {
		return false, err
	}

	// Update cache state
	cm.mu.Lock()
	// Fetch state while holding lock
	state := cm.getOrCreateCacheStateUnsafe(serverName)
	state.LastTurnSize = turnSize
	state.LastMemoryPressure = memoryPressure
	if shouldFlush {
		state.IsFlushed = true
	}
	cm.mu.Unlock()

	if shouldFlush {
		zap.L().Debug("Cache flush decision: flush",
			zap.String("server_name", serverName),
			zap.Int("turn_size", turnSize),
			zap.Float64("memory_pressure", memoryPressure),
			zap.Bool("is_final_iteration", isFinalIteration))
	} else {
		zap.L().Debug("Cache flush decision: preserve",
			zap.String("server_name", serverName),
			zap.Int("turn_size", turnSize),
			zap.Float64("memory_pressure", memoryPressure),
			zap.Bool("is_final_iteration", isFinalIteration))
	}

	return shouldFlush, nil
}

// MarkFlushed marks the cache as flushed for an LLM server
func (cm *CacheManager) MarkFlushed(serverName string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	state := cm.getOrCreateCacheStateUnsafe(serverName)
	state.IsFlushed = true
}

// MarkPreserved marks the cache as preserved for an LLM server
func (cm *CacheManager) MarkPreserved(serverName string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	state := cm.getOrCreateCacheStateUnsafe(serverName)
	state.IsFlushed = false
}

// FlushCache actually flushes the cache for a server using its cache controller
func (cm *CacheManager) FlushCache(ctx context.Context, serverManager *LLMServerManager, serverName string) error {
	// Get cache controller from server manager
	controller, err := serverManager.GetCacheController(serverName)
	if err != nil {
		return fmt.Errorf("failed to get cache controller for server '%s': %w", serverName, err)
	}

	// If controller is nil, cache control is not supported for this backend
	if controller == nil {
		zap.L().Debug("Cache controller not available for server, skipping flush",
			zap.String("server_name", serverName))
		// Still mark as flushed in our state tracking
		cm.MarkFlushed(serverName)
		return nil
	}

	// Actually flush the cache
	if err := controller.FlushCache(ctx); err != nil {
		return fmt.Errorf("failed to flush cache for server '%s': %w", serverName, err)
	}

	// Update our state
	cm.MarkFlushed(serverName)

	zap.L().Info("Flushed cache for LLM server",
		zap.String("server_name", serverName))

	return nil
}
