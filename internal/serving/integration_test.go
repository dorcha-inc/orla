package serving

import (
	"context"
	"strings"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// test env var name, not a credential. //nolint:gosec // G101
const testAPIKeyEnvVar = "ORLA_TEST_OPENAI_KEY" //nolint:gosec // G101 - env var name, not a credential

// Integration tests use MockLLMServer for end-to-end flows without external services.
// Run with: make test-integration

func TestIntegration_Execute_EndToEnd(t *testing.T) {
	srv := model.NewMockLLMServer().
		ReturnContent("integration test response").
		Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(nil)
	layer.AddLLMBackend("integration-backend", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model")

	ctx := context.Background()
	response, err := layer.Execute(ctx, "integration-backend", "default", []model.Message{
		{Role: model.MessageRoleUser, Content: "hello"},
	}, nil, model.InferenceOptions{Stream: false})
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "integration test response", response.Content)
}

func TestIntegration_ExecuteStream_EndToEnd(t *testing.T) {
	srv := model.NewMockLLMServer().
		ReturnStreamChunks([]string{"stream", "ing", " response"}).
		Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(nil)
	layer.AddLLMBackend("integration-backend", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model")

	ctx := context.Background()
	response, ch, err := layer.ExecuteStream(ctx, "integration-backend", "default", []model.Message{
		{Role: model.MessageRoleUser, Content: "stream me"},
	}, nil, model.InferenceOptions{Stream: true})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, ch)

	var chunks []string
	for ev := range ch {
		if contentEv, ok := ev.(*model.ContentEvent); ok {
			chunks = append(chunks, contentEv.Content)
		}
	}
	assert.Equal(t, []string{"stream", "ing", " response"}, chunks)
	assert.Equal(t, "streaming response", response.Content)
}

func TestIntegration_Execute_WithToolCalls(t *testing.T) {
	srv := model.NewMockLLMServer().
		ReturnContent("").
		ReturnToolCall("get_weather", `{"location":"Boston"}`).
		Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(nil)
	layer.AddLLMBackend("integration-backend", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model")

	ctx := context.Background()
	response, err := layer.Execute(ctx, "integration-backend", "default", []model.Message{
		{Role: model.MessageRoleUser, Content: "what's the weather?"},
	}, nil, model.InferenceOptions{Stream: false})
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "get_weather", response.ToolCalls[0].Name)
}

func TestIntegration_Execute_BackendNotFound(t *testing.T) {
	layer := NewAgenticLayer(nil)
	ctx := context.Background()

	_, err := layer.Execute(ctx, "nonexistent-backend", "", []model.Message{
		{Role: model.MessageRoleUser, Content: "hi"},
	}, nil, model.InferenceOptions{Stream: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestIntegration_Execute_MultipleBackends(t *testing.T) {
	srv1 := model.NewMockLLMServer().ReturnContent("from-backend-1").Start()
	t.Cleanup(srv1.Close)
	srv2 := model.NewMockLLMServer().ReturnContent("from-backend-2").Start()
	t.Cleanup(srv2.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(nil)
	layer.AddLLMBackend("backend-1", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv1.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:model-a")
	layer.AddLLMBackend("backend-2", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv2.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:model-b")

	ctx := context.Background()

	resp1, err := layer.Execute(ctx, "backend-1", "stage-a", []model.Message{
		{Role: model.MessageRoleUser, Content: "q1"},
	}, nil, model.InferenceOptions{Stream: false})
	require.NoError(t, err)
	assert.Equal(t, "from-backend-1", resp1.Content)

	resp2, err := layer.Execute(ctx, "backend-2", "stage-b", []model.Message{
		{Role: model.MessageRoleUser, Content: "q2"},
	}, nil, model.InferenceOptions{Stream: false})
	require.NoError(t, err)
	assert.Equal(t, "from-backend-2", resp2.Content)

	backends := layer.ListLLMBackends()
	assert.Contains(t, backends, "backend-1")
	assert.Contains(t, backends, "backend-2")
}

func TestIntegration_Execute_InvalidAPIKey(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("ok").Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "") // Explicitly empty so provider fails
	layer := NewAgenticLayer(nil)
	layer.AddLLMBackend("backend", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:model")

	ctx := context.Background()
	_, err := layer.Execute(ctx, "backend", "", []model.Message{
		{Role: model.MessageRoleUser, Content: "hi"},
	}, nil, model.InferenceOptions{Stream: false})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "API key") || strings.Contains(err.Error(), "required"),
		"expected API key error, got: %s", err.Error())
}
