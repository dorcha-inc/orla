package serving

import (
	"context"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCachePolicyEvaluator(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyPreserve,
			},
		},
		{
			Name: "server2",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyAggressiveFlush,
			},
		},
		{
			Name: "server3",
			// No cache config
		},
	}

	evaluator := NewCachePolicyEvaluator(servers)
	require.NotNil(t, evaluator)
	assert.NotNil(t, evaluator.policies)
	assert.Equal(t, config.CachePolicyPreserve, evaluator.policies["server1"].Policy)
	assert.Equal(t, config.CachePolicyAggressiveFlush, evaluator.policies["server2"].Policy)
	assert.NotContains(t, evaluator.policies, "server3")
}

func TestCachePolicyEvaluator_EvaluateDecision_Preserve(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyPreserve,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Preserve policy should always return false (don't flush)
	shouldFlush, err := evaluator.EvaluateDecision("server1", 1000, 0.9, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// Even with final iteration, preserve should still preserve (unless flush_after_final is set)
	shouldFlush, err = evaluator.EvaluateDecision("server1", 1000, 0.9, true)
	require.NoError(t, err)
	assert.False(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_PreserveWithFlushAfterFinal(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy:         config.CachePolicyPreserve,
				FlushAfterFinal: true,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Should preserve normally
	shouldFlush, err := evaluator.EvaluateDecision("server1", 1000, 0.9, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// Should flush on final iteration
	shouldFlush, err = evaluator.EvaluateDecision("server1", 1000, 0.9, true)
	require.NoError(t, err)
	assert.True(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_AggressiveFlush(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyAggressiveFlush,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Aggressive flush should always return true
	shouldFlush, err := evaluator.EvaluateDecision("server1", 10, 0.1, false)
	require.NoError(t, err)
	assert.True(t, shouldFlush)

	shouldFlush, err = evaluator.EvaluateDecision("server1", 1000, 0.9, true)
	require.NoError(t, err)
	assert.True(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_PreserveOnSmallTurns(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy:            config.CachePolicyPreserveOnSmallTurns,
				SmallTurnThreshold: 100,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Small turn should preserve
	shouldFlush, err := evaluator.EvaluateDecision("server1", 50, 0.5, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// Large turn should flush
	shouldFlush, err = evaluator.EvaluateDecision("server1", 150, 0.5, false)
	require.NoError(t, err)
	assert.True(t, shouldFlush)

	// Exactly at threshold should flush
	shouldFlush, err = evaluator.EvaluateDecision("server1", 100, 0.5, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush) // threshold is exclusive
}

func TestCachePolicyEvaluator_EvaluateDecision_PreserveOnSmallTurns_DefaultThreshold(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyPreserveOnSmallTurns,
				// No threshold set, should use default 100
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Small turn should preserve
	shouldFlush, err := evaluator.EvaluateDecision("server1", 50, 0.5, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// Large turn should flush
	shouldFlush, err = evaluator.EvaluateDecision("server1", 150, 0.5, false)
	require.NoError(t, err)
	assert.True(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_FlushUnderPressure(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy:                config.CachePolicyFlushUnderPressure,
				MemoryPressureThreshold: 0.8,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Low pressure should preserve
	shouldFlush, err := evaluator.EvaluateDecision("server1", 1000, 0.5, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// High pressure should flush
	shouldFlush, err = evaluator.EvaluateDecision("server1", 1000, 0.9, false)
	require.NoError(t, err)
	assert.True(t, shouldFlush)

	// Exactly at threshold should not flush (exclusive)
	shouldFlush, err = evaluator.EvaluateDecision("server1", 1000, 0.8, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_FlushUnderPressure_DefaultThreshold(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyFlushUnderPressure,
				// No threshold set, should use default 0.85
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// Below default threshold should preserve
	shouldFlush, err := evaluator.EvaluateDecision("server1", 1000, 0.8, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// Above default threshold should flush
	shouldFlush, err = evaluator.EvaluateDecision("server1", 1000, 0.9, false)
	require.NoError(t, err)
	assert.True(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_NoPolicy(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			// No cache config
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	// No policy should default to preserve
	shouldFlush, err := evaluator.EvaluateDecision("server1", 1000, 0.9, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)
}

func TestCachePolicyEvaluator_EvaluateDecision_UnknownPolicy(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: "unknown_policy",
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)

	shouldFlush, err := evaluator.EvaluateDecision("server1", 1000, 0.9, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown cache policy")
	assert.False(t, shouldFlush)
}

func TestNewCacheManager(t *testing.T) {
	evaluator := NewCachePolicyEvaluator([]*config.LLMServerConfig{})
	manager := NewCacheManager(evaluator)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.evaluator)
	assert.NotNil(t, manager.cacheState)
}

func TestCacheManager_GetOrCreateCacheState(t *testing.T) {
	evaluator := NewCachePolicyEvaluator([]*config.LLMServerConfig{})
	manager := NewCacheManager(evaluator)

	// First call should create new state
	state1 := manager.GetOrCreateCacheState("server1")
	require.NotNil(t, state1)
	assert.Equal(t, "server1", state1.ServerName)
	assert.False(t, state1.IsFlushed)

	// Second call should return same state
	state2 := manager.GetOrCreateCacheState("server1")
	assert.Equal(t, state1, state2)

	// Different server should create new state
	state3 := manager.GetOrCreateCacheState("server2")
	require.NotNil(t, state3)
	assert.Equal(t, "server2", state3.ServerName)
	assert.NotEqual(t, state1, state3)
}

func TestCacheManager_ShouldFlush(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyAggressiveFlush,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)
	manager := NewCacheManager(evaluator)

	ctx := context.Background()
	shouldFlush, err := manager.ShouldFlush(ctx, "server1", 100, 0.5, false)
	require.NoError(t, err)
	assert.True(t, shouldFlush)

	// Check that state was updated
	state := manager.GetOrCreateCacheState("server1")
	assert.True(t, state.IsFlushed)
	assert.Equal(t, 100, state.LastTurnSize)
	assert.Equal(t, 0.5, state.LastMemoryPressure)
}

func TestCacheManager_ShouldFlush_Preserve(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Cache: &config.CacheConfig{
				Policy: config.CachePolicyPreserve,
			},
		},
	}
	evaluator := NewCachePolicyEvaluator(servers)
	manager := NewCacheManager(evaluator)

	ctx := context.Background()
	shouldFlush, err := manager.ShouldFlush(ctx, "server1", 100, 0.5, false)
	require.NoError(t, err)
	assert.False(t, shouldFlush)

	// Check that state was updated but not flushed
	state := manager.GetOrCreateCacheState("server1")
	assert.False(t, state.IsFlushed)
	assert.Equal(t, 100, state.LastTurnSize)
}

func TestCacheManager_MarkFlushed(t *testing.T) {
	evaluator := NewCachePolicyEvaluator([]*config.LLMServerConfig{})
	manager := NewCacheManager(evaluator)

	state := manager.GetOrCreateCacheState("server1")
	assert.False(t, state.IsFlushed)

	manager.MarkFlushed("server1")
	state = manager.GetOrCreateCacheState("server1")
	assert.True(t, state.IsFlushed)
}

func TestCacheManager_MarkPreserved(t *testing.T) {
	evaluator := NewCachePolicyEvaluator([]*config.LLMServerConfig{})
	manager := NewCacheManager(evaluator)

	state := manager.GetOrCreateCacheState("server1")
	state.IsFlushed = true
	assert.True(t, state.IsFlushed)

	manager.MarkPreserved("server1")
	state = manager.GetOrCreateCacheState("server1")
	assert.False(t, state.IsFlushed)
}
