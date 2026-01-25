package serving

import (
	"context"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test implementation of model.Provider
type mockProvider struct {
	name      string
	maxTokens *int
	lastMaxTokens *int
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Chat(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool, maxTokens *int) (*model.Response, <-chan model.StreamEvent, error) {
	m.lastMaxTokens = maxTokens
	return &model.Response{
		Content: "test response",
	}, nil, nil
}

func (m *mockProvider) EnsureReady(ctx context.Context) error {
	return nil
}

func TestLayer_ExecuteTask_WithMaxTokens(t *testing.T) {
	maxTokens := 42
	cfg := &config.AgenticServingConfig{
		LLMServers: []*config.LLMServerConfig{
			{
				Name: "test-server",
				Backend: &core.LLMBackend{
					Type:     core.LLMInferenceAPITypeOllama,
					Endpoint: "http://localhost:11434",
				},
				Model: "test-model",
			},
		},
		AgentProfiles: []*config.AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*config.Workflow{
			{
				Name: "test-workflow",
				Tasks: []*config.WorkflowTask{
					{
						AgentProfile: "test-profile",
					},
				},
			},
		},
	}

	layer, err := NewLayer(cfg)
	require.NoError(t, err)

	// Replace the provider with a mock (providers are stored by server name)
	mock := &mockProvider{name: "mock"}
	layer.serverManager.mu.Lock()
	layer.serverManager.providers["test-server"] = mock
	layer.serverManager.mu.Unlock()

	execution, err := layer.StartWorkflow(context.Background(), "test-workflow")
	require.NoError(t, err)

	response, err := layer.ExecuteTask(context.Background(), execution, 0, "test prompt", &maxTokens)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "test response", response.Content)
	
	// Verify that maxTokens was passed to the provider
	assert.NotNil(t, mock.lastMaxTokens)
	assert.Equal(t, maxTokens, *mock.lastMaxTokens)
}

func TestLayer_ExecuteTask_WithoutMaxTokens(t *testing.T) {
	cfg := &config.AgenticServingConfig{
		LLMServers: []*config.LLMServerConfig{
			{
				Name: "test-server",
				Backend: &core.LLMBackend{
					Type:     core.LLMInferenceAPITypeOllama,
					Endpoint: "http://localhost:11434",
				},
				Model: "test-model",
			},
		},
		AgentProfiles: []*config.AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*config.Workflow{
			{
				Name: "test-workflow",
				Tasks: []*config.WorkflowTask{
					{
						AgentProfile: "test-profile",
					},
				},
			},
		},
	}

	layer, err := NewLayer(cfg)
	require.NoError(t, err)

	// Replace the provider with a mock (providers are stored by server name)
	mock := &mockProvider{name: "mock"}
	layer.serverManager.mu.Lock()
	layer.serverManager.providers["test-server"] = mock
	layer.serverManager.mu.Unlock()

	execution, err := layer.StartWorkflow(context.Background(), "test-workflow")
	require.NoError(t, err)

	response, err := layer.ExecuteTask(context.Background(), execution, 0, "test prompt", nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "test response", response.Content)
	
	// Verify that maxTokens was nil when not provided
	assert.Nil(t, mock.lastMaxTokens)
}

func TestLayer_ExecuteTask_WithMaxTokensZero(t *testing.T) {
	maxTokens := 0
	cfg := &config.AgenticServingConfig{
		LLMServers: []*config.LLMServerConfig{
			{
				Name: "test-server",
				Backend: &core.LLMBackend{
					Type:     core.LLMInferenceAPITypeOllama,
					Endpoint: "http://localhost:11434",
				},
				Model: "test-model",
			},
		},
		AgentProfiles: []*config.AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*config.Workflow{
			{
				Name: "test-workflow",
				Tasks: []*config.WorkflowTask{
					{
						AgentProfile: "test-profile",
					},
				},
			},
		},
	}

	layer, err := NewLayer(cfg)
	require.NoError(t, err)

	// Replace the provider with a mock (providers are stored by server name)
	mock := &mockProvider{name: "mock"}
	layer.serverManager.mu.Lock()
	layer.serverManager.providers["test-server"] = mock
	layer.serverManager.mu.Unlock()

	execution, err := layer.StartWorkflow(context.Background(), "test-workflow")
	require.NoError(t, err)

	response, err := layer.ExecuteTask(context.Background(), execution, 0, "test prompt", &maxTokens)
	require.NoError(t, err)
	assert.NotNil(t, response)
	
	// Verify that maxTokens was passed even when 0
	assert.NotNil(t, mock.lastMaxTokens)
	assert.Equal(t, 0, *mock.lastMaxTokens)
}
