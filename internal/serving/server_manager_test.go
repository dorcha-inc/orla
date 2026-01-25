package serving

import (
	"context"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLLMServerManager(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name:  "server1",
			Model: "model1",
		},
		{
			Name:  "server2",
			Model: "model2",
		},
	}

	manager := NewLLMServerManager(servers)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.servers)
	assert.NotNil(t, manager.providers)
	assert.NotNil(t, manager.cacheControllers)

	// Should be able to get server configs
	config1, err := manager.GetServerConfig("server1")
	require.NoError(t, err)
	assert.Equal(t, "server1", config1.Name)

	config2, err := manager.GetServerConfig("server2")
	require.NoError(t, err)
	assert.Equal(t, "server2", config2.Name)
}

func TestNewLLMServerManager_Empty(t *testing.T) {
	manager := NewLLMServerManager([]*config.LLMServerConfig{})
	require.NotNil(t, manager)
	assert.NotNil(t, manager.servers)
	assert.Len(t, manager.servers, 0)
}

func TestLLMServerManager_GetServerConfig(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name:  "server1",
			Model: "model1",
		},
	}
	manager := NewLLMServerManager(servers)

	config, err := manager.GetServerConfig("server1")
	require.NoError(t, err)
	assert.Equal(t, "server1", config.Name)
	assert.Equal(t, "model1", config.Model)
}

func TestLLMServerManager_GetServerConfig_NotFound(t *testing.T) {
	manager := NewLLMServerManager([]*config.LLMServerConfig{})

	config, err := manager.GetServerConfig("nonexistent")
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "not found")
}

func TestLLMServerManager_ListServers(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
		},
		{
			Name: "server2",
		},
		{
			Name: "server3",
		},
	}
	manager := NewLLMServerManager(servers)

	serverList := manager.ListServers()
	require.Len(t, serverList, 3)
	assert.Contains(t, serverList, "server1")
	assert.Contains(t, serverList, "server2")
	assert.Contains(t, serverList, "server3")
}

func TestLLMServerManager_ListServers_Empty(t *testing.T) {
	manager := NewLLMServerManager([]*config.LLMServerConfig{})

	serverList := manager.ListServers()
	assert.Len(t, serverList, 0)
}

func TestLLMServerManager_GetProvider(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeOllama,
				Endpoint: "http://localhost:11434",
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	ctx := context.Background()
	provider, err := manager.GetProvider(ctx, "server1")
	// This might fail if Ollama is not running, but we can test the structure
	if err != nil {
		// Provider creation might fail, but we can verify the error handling
		assert.Contains(t, err.Error(), "server1")
	} else {
		require.NotNil(t, provider)
		// Second call should return cached provider
		provider2, err2 := manager.GetProvider(ctx, "server1")
		require.NoError(t, err2)
		assert.Equal(t, provider, provider2)
	}
}

func TestLLMServerManager_GetProvider_NotFound(t *testing.T) {
	manager := NewLLMServerManager([]*config.LLMServerConfig{})

	ctx := context.Background()
	provider, err := manager.GetProvider(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "not found")
}

func TestLLMServerManager_GetProvider_Caching(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeOllama,
				Endpoint: "http://localhost:11434",
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	ctx := context.Background()
	
	// First call creates provider
	provider1, err1 := manager.GetProvider(ctx, "server1")
	if err1 == nil {
		// Second call should return same cached provider
		provider2, err2 := manager.GetProvider(ctx, "server1")
		require.NoError(t, err2)
		assert.Equal(t, provider1, provider2)
	}
}

func TestLLMServerManager_GetCacheController_SGLang(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeSGLang,
				Endpoint: "http://localhost:30000",
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	controller, err := manager.GetCacheController("server1")
	require.NoError(t, err)
	require.NotNil(t, controller)

	// Second call should return cached controller
	controller2, err2 := manager.GetCacheController("server1")
	require.NoError(t, err2)
	assert.Equal(t, controller, controller2)
}

func TestLLMServerManager_GetCacheController_OpenAI(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeOpenAI,
				Endpoint: "http://localhost:8000",
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	// OpenAI doesn't support cache control, should return nil, nil
	controller, err := manager.GetCacheController("server1")
	require.NoError(t, err)
	assert.Nil(t, controller)
}

func TestLLMServerManager_GetCacheController_NotFound(t *testing.T) {
	manager := NewLLMServerManager([]*config.LLMServerConfig{})

	controller, err := manager.GetCacheController("nonexistent")
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "not found")
}

func TestLLMServerManager_GetHealthStatus_NotFound(t *testing.T) {
	manager := NewLLMServerManager([]*config.LLMServerConfig{})

	ctx := context.Background()
	status, err := manager.GetHealthStatus(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Equal(t, HealthStatusUnavailable, status)
	assert.Contains(t, err.Error(), "not found")
}

func TestLLMServerManager_GetHealthStatus_ProviderError(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeOllama,
				Endpoint: "http://invalid-host:99999", // Invalid endpoint
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	ctx := context.Background()
	status, err := manager.GetHealthStatus(ctx, "server1")
	// Should return unavailable status
	assert.Equal(t, HealthStatusUnavailable, status)
	// Error might be about provider creation or health check failure
	assert.Error(t, err)
}

func TestLLMServerManager_ConcurrentAccess(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeOllama,
				Endpoint: "http://localhost:11434",
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	// Test concurrent access to GetServerConfig
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			config, err := manager.GetServerConfig("server1")
			require.NoError(t, err)
			assert.NotNil(t, config)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test concurrent access to ListServers
	done2 := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			serverList := manager.ListServers()
			assert.Len(t, serverList, 1)
			done2 <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done2
	}
}

func TestLLMServerManager_GetCacheController_Caching(t *testing.T) {
	servers := []*config.LLMServerConfig{
		{
			Name: "server1",
			Backend: &core.LLMBackend{
				Type:     core.LLMInferenceAPITypeSGLang,
				Endpoint: "http://localhost:30000",
			},
			Model: "test-model",
		},
	}
	manager := NewLLMServerManager(servers)

	controller1, err1 := manager.GetCacheController("server1")
	require.NoError(t, err1)
	require.NotNil(t, controller1)

	// Second call should return cached controller
	controller2, err2 := manager.GetCacheController("server1")
	require.NoError(t, err2)
	assert.Equal(t, controller1, controller2)
}
