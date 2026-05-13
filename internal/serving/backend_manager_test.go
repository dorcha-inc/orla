package serving

import (
	"context"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLLMServerManager(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.backends)
	assert.NotNil(t, manager.providers)
	assert.NotNil(t, manager.executors)
}

func TestLLMServerManager_AddServer(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)
	require.NoError(t, manager.AddLLMBackend("server1", &core.LLMBackend{
				Endpoint: "http://localhost:11434/v1",
	}, "openai:test-model"))

	servers := manager.ListLLMBackends()
	require.Len(t, servers, 1)
	assert.Equal(t, "server1", servers[0])
}

func TestLLMServerManager_ListServers(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)
	require.NoError(t, manager.AddLLMBackend("server1", &core.LLMBackend{Endpoint: "http://localhost:11434/v1"}, "openai:m1"))
	require.NoError(t, manager.AddLLMBackend("server2", &core.LLMBackend{Endpoint: "http://localhost:11434/v1"}, "openai:m2"))
	require.NoError(t, manager.AddLLMBackend("server3", &core.LLMBackend{Endpoint: "http://localhost:11434/v1"}, "openai:m3"))

	servers := manager.ListLLMBackends()
	require.Len(t, servers, 3)
	assert.Contains(t, servers, "server1")
	assert.Contains(t, servers, "server2")
	assert.Contains(t, servers, "server3")
}

func TestLLMServerManager_ListServers_Empty(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)
	servers := manager.ListLLMBackends()
	assert.Len(t, servers, 0)
}

func TestLLMServerManager_GetProvider(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)
	require.NoError(t, manager.AddLLMBackend("server1", &core.LLMBackend{
				Endpoint: "http://localhost:11434/v1",
	}, "openai:test-model"))

	ctx := context.Background()
	provider, err := manager.GetModelProvider(ctx, "server1")
	if err != nil {
		assert.Contains(t, err.Error(), "server1")
	} else {
		require.NotNil(t, provider)
		provider2, err2 := manager.GetModelProvider(ctx, "server1")
		require.NoError(t, err2)
		assert.Equal(t, provider, provider2)
	}
}

func TestLLMServerManager_GetProvider_NotFound(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)

	ctx := context.Background()
	provider, err := manager.GetModelProvider(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "not found")
}

func TestLLMServerManager_GetHealthStatus_NotFound(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)

	ctx := context.Background()
	status, err := manager.GetHealthStatus(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Equal(t, HealthStatusUnavailable, status)
	assert.Contains(t, err.Error(), "not found")
}

func TestLLMServerManager_GetHealthStatus_ProviderError(t *testing.T) {
	// OpenAI provider's EnsureReady is a no-op (no health check), so it returns healthy.
	// Connection errors surface on first inference request instead.
	manager := NewLLMBackendManager(nil, nil)
	require.NoError(t, manager.AddLLMBackend("server1", &core.LLMBackend{
				Endpoint: "http://invalid-host:99999/v1",
	}, "openai:test-model"))

	ctx := context.Background()
	status, err := manager.GetHealthStatus(ctx, "server1")
	assert.Equal(t, HealthStatusHealthy, status)
	assert.NoError(t, err)
}

func TestLLMServerManager_ConcurrentAccess(t *testing.T) {
	manager := NewLLMBackendManager(nil, nil)
	require.NoError(t, manager.AddLLMBackend("server1", &core.LLMBackend{
				Endpoint: "http://localhost:11434/v1",
	}, "openai:test-model"))

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			servers := manager.ListLLMBackends()
			assert.Len(t, servers, 1)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
