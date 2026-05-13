package serving

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving/access"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLayer_NewLayer(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NotNil(t, layer)
	assert.Empty(t, layer.ListLLMBackends())
}

func TestLayer_AddServer(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.AddLLMBackend("test-server", &core.LLMBackend{
				Endpoint: "http://localhost:11434/v1",
	}, "openai:test-model"))
	assert.Contains(t, layer.ListLLMBackends(), "test-server")
}

func TestLayer_Execute_WithMaxTokens(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("test response").Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.AddLLMBackend("test-server", &core.LLMBackend{
				Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model"))

	response, err := layer.Execute(context.Background(), "test-server", "test", []model.Message{
		{Role: model.MessageRoleUser, Content: "test prompt"},
	}, nil, model.InferenceOptions{MaxTokens: core.Ptr(42)})
	require.NoError(t, err)
	assert.Equal(t, "test response", response.Content)

	var req struct {
		MaxTokens *int `json:"max_tokens"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	require.NotNil(t, req.MaxTokens)
	assert.Equal(t, 42, *req.MaxTokens)
}

func TestLayer_Execute_WithoutMaxTokens(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("test response").Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.AddLLMBackend("test-server", &core.LLMBackend{
				Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model"))

	response, err := layer.Execute(context.Background(), "test-server", "test", []model.Message{
		{Role: model.MessageRoleUser, Content: "test prompt"},
	}, nil, model.InferenceOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test response", response.Content)

	var req struct {
		MaxTokens *int `json:"max_tokens"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	assert.Nil(t, req.MaxTokens)
}

func TestLayer_Execute_ServerNotFound(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	_, err := layer.Execute(context.Background(), "nonexistent", "", nil, nil, model.InferenceOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLayer_Execute_RejectsStream(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("ignored").Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.AddLLMBackend("test-server", &core.LLMBackend{
				Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model"))

	_, err := layer.Execute(context.Background(), "test-server", "test", []model.Message{
		{Role: model.MessageRoleUser, Content: "test"},
	}, nil, model.InferenceOptions{Stream: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ExecuteStream")
}

func TestLayer_ExecuteStream(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnStreamChunks([]string{"test ", "response"}).Start()
	t.Cleanup(srv.Close)

	t.Setenv(testAPIKeyEnvVar, "test-key")
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.AddLLMBackend("test-server", &core.LLMBackend{
				Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model"))

	response, ch, err := layer.ExecuteStream(context.Background(), "test-server", "test", []model.Message{
		{Role: model.MessageRoleUser, Content: "test"},
	}, nil, model.InferenceOptions{Stream: true, MaxTokens: core.Ptr(10)})
	require.NoError(t, err)
	require.NotNil(t, ch)
	for range ch {
	}
	assert.Equal(t, "test response", response.Content)
}

// ---- ValidateAccess tests ----

func TestValidateAccess_NoTags(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	d := layer.ValidateAccess(nil, "cheap", nil, nil)
	assert.True(t, d.Allowed)
}

func TestValidateAccess_BackendAllowed(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "eng-allow", Subjects: []string{"tenant:engineering"}, Resources: []string{"backend:cheap"}, Action: access.ActionAllow,
	}))
	d := layer.ValidateAccess(map[string]string{"tenant": "engineering"}, "cheap", nil, nil)
	assert.True(t, d.Allowed)
}

func TestValidateAccess_BackendDenied(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "intern-allow-cheap", Subjects: []string{"tenant:interns"}, Resources: []string{"backend:cheap"}, Action: access.ActionAllow,
	}))
	d := layer.ValidateAccess(map[string]string{"tenant": "interns"}, "strong", nil, nil)
	assert.False(t, d.Allowed)
	assert.Contains(t, d.Reason, "backend")
}

func TestValidateAccess_ToolDenied(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "allow-tools", Subjects: []string{"tenant:eng"}, Resources: []string{"tool:*"}, Action: access.ActionAllow,
	}))
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "deny-shell", Subjects: []string{"tenant:eng"}, Resources: []string{"tool:shell"}, Action: access.ActionDeny,
	}))
	d := layer.ValidateAccess(map[string]string{"tenant": "eng"}, "", []string{"shell"}, nil)
	assert.False(t, d.Allowed)
	assert.Contains(t, d.Reason, "tool")
}

func TestValidateAccess_DataLabelDenied(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "pii-deny", Subjects: []string{"backend:ext"}, Resources: []string{"data:pii"}, Action: access.ActionDeny,
	}))
	d := layer.ValidateAccess(map[string]string{"tenant": "hr"}, "ext", nil, []string{"pii"})
	assert.False(t, d.Allowed)
	assert.Contains(t, d.Reason, "data access denied")
}

func TestValidateAccess_HappyPath(t *testing.T) {
	layer := NewAgenticLayer(AgenticLayerOptions{})
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "eng-all", Subjects: []string{"tenant:engineering"}, Resources: []string{"backend:*"}, Action: access.ActionAllow,
	}))
	d := layer.ValidateAccess(map[string]string{"tenant": "engineering"}, "cheap", nil, nil)
	assert.True(t, d.Allowed)
}
