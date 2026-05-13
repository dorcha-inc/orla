package model

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/core"
)

const testAPIKeyEnvVar = "ORLA_TEST_OPENAI_KEY" //nolint:gosec // G101 - env var name, not a credential

func TestConvertToolsToOpenAIParams_PassesParametersThrough(t *testing.T) {
	t.Parallel()

	params := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)
	got, err := convertToolsToOpenAIParams([]*Tool{
		{Name: "t1", Description: "d1", Parameters: params},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "t1", got[0].Function.Name)
	require.Equal(t, "d1", got[0].Function.Description.Value)
	require.Equal(t, "object", got[0].Function.Parameters["type"])
}

func TestConvertToolsToOpenAIParams_OmitsEmptyParameters(t *testing.T) {
	t.Parallel()

	got, err := convertToolsToOpenAIParams([]*Tool{{Name: "t1"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Nil(t, got[0].Function.Parameters)
}

func TestConvertToolsToOpenAIParams_InvalidParametersJSONErrors(t *testing.T) {
	t.Parallel()

	_, err := convertToolsToOpenAIParams([]*Tool{
		{Name: "t1", Parameters: json.RawMessage(`{not valid`)},
	})
	require.Error(t, err)
}

func TestConvertMessagesToOpenAI_UnsupportedRoleErrors(t *testing.T) {
	t.Parallel()

	_, err := convertMessagesToOpenAI([]Message{{Role: "developer", Content: "x"}})
	require.Error(t, err)
}

func TestConvertOpenAIToolCalls_PassesArgumentsThrough(t *testing.T) {
	t.Parallel()

	calls := []openai.ChatCompletionMessageToolCall{
		{ID: "abc", Function: openai.ChatCompletionMessageToolCallFunction{Name: "do", Arguments: `{"x":"y"}`}},
	}

	got, err := convertOpenAIToolCalls(calls)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "abc", got[0].ID)
	require.Equal(t, "do", got[0].Name)
	require.JSONEq(t, `{"x":"y"}`, string(got[0].Arguments))
}

func TestConvertOpenAIToolCalls_EmptyIDReturnsError(t *testing.T) {
	t.Parallel()

	calls := []openai.ChatCompletionMessageToolCall{
		{ID: "", Function: openai.ChatCompletionMessageToolCallFunction{Name: "tool", Arguments: `{}`}},
	}

	got, err := convertOpenAIToolCalls(calls)
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "empty id")
}

func TestGetOpenAICompatibleEndpoint_Validation(t *testing.T) {
	t.Parallel()

	_, _, err := getOpenAICompatibleEndpoint(nil)
	require.Error(t, err)

	_, _, err = getOpenAICompatibleEndpoint(&core.LLMBackend{Endpoint: "http://example"})
	require.NoError(t, err)
}

func TestNewOpenAIProvider_RequiresAPIKeyEnvVarValue(t *testing.T) {
	t.Parallel()

	// API key env var is not set => should error.
	llmBackend := &core.LLMBackend{
		Endpoint:     "http://example",
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	_, err := NewOpenAIProvider("model", llmBackend)
	require.Error(t, err)
}

func TestOpenAIProvider_Chat_NonStreaming_BasicAndToolCalls(t *testing.T) {
	srv := NewMockLLMServer().
		ReturnContent("hello").
		ReturnToolCallWithID("call_abc", "do", `{"x":"y"}`).
		Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("m", llmBackend)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: false})
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "hello", resp.Content)
	require.Len(t, resp.ToolCalls, 1)
	require.Equal(t, "call_abc", resp.ToolCalls[0].ID)
	require.Equal(t, "do", resp.ToolCalls[0].Name)
}

func TestOpenAIProvider_Chat_Streaming_Content(t *testing.T) {
	srv := NewMockLLMServer().
		ReturnStreamChunks([]string{"he", "llo"}).
		Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("m", llmBackend)
	require.NoError(t, err)

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: true})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, ch)

	// Drain channel to let goroutine finish without leaks.
	for range ch {
	}
	// Response content is built incrementally by streaming handler; should include all chunks.
	require.Equal(t, "hello", resp.Content)
}

func TestOpenAIProvider_Chat_WithMaxTokens(t *testing.T) {
	maxTokens := 100
	srv := NewMockLLMServer().ReturnContent("Short response").Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "test-key")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("test-model", llmBackend)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: false, MaxTokens: core.Ptr(maxTokens)})
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "Short response", resp.Content)

	var req struct {
		MaxTokens *int `json:"max_tokens"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	require.NotNil(t, req.MaxTokens)
	require.Equal(t, maxTokens, *req.MaxTokens)
}

func TestOpenAIProvider_Chat_WithoutMaxTokens(t *testing.T) {
	srv := NewMockLLMServer().ReturnContent("Response").Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "test-key")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("test-model", llmBackend)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: false})
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "Response", resp.Content)

	var req struct {
		MaxTokens *int `json:"max_tokens"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	require.Nil(t, req.MaxTokens)
}

func TestOpenAIProvider_Chat_WithMaxTokensZero(t *testing.T) {
	maxTokens := 0
	srv := NewMockLLMServer().ReturnContent("").Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "test-key")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("test-model", llmBackend)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: false, MaxTokens: &maxTokens})
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)

	var req struct {
		MaxTokens *int `json:"max_tokens"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	require.NotNil(t, req.MaxTokens)
	require.Equal(t, 0, *req.MaxTokens)
}

func TestOpenAIProvider_Chat_Streaming_WithToolCalls(t *testing.T) {
	srv := NewMockLLMServer().
		ReturnStreamWithToolCalls("hi", mockLLMToolCall{ID: "call_1", Name: "tool", Args: `{"x":"y"}`}).
		Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")

	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("m", llmBackend)
	require.NoError(t, err)

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: true})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, ch)

	// Drain channel
	for range ch {
	}

	require.Equal(t, "hi", resp.Content)
	require.Len(t, resp.ToolCalls, 1)
	require.Equal(t, "call_1", resp.ToolCalls[0].ID)
	require.Equal(t, "tool", resp.ToolCalls[0].Name)
}

func TestOpenAIProvider_Chat_NonStreaming_NoChoices(t *testing.T) {
	srv := NewMockLLMServer().ReturnNoChoices().Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")

	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("m", llmBackend)
	require.NoError(t, err)

	_, _, err = p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: false})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no choices")
}

func TestGetOpenAICompatibleEndpoint_MissingEndpoint(t *testing.T) {
	t.Parallel()

	_, _, err := getOpenAICompatibleEndpoint(
		&core.LLMBackend{
			Endpoint: "",
					})
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint is required")
}

func TestOpenAIProvider_Chat_WithResponseFormat_NonStreaming(t *testing.T) {
	minimalSchema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`)
	srv := NewMockLLMServer().ReturnContent(`{"answer":"hello"}`).Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("m", llmBackend)
	require.NoError(t, err)

	opts := InferenceOptions{
		Stream: false,
		ResponseFormat: &StructuredOutputOptions{
			Name:   "test-schema",
			Strict: true,
			Schema: minimalSchema,
		},
	}
	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "Say hello in JSON"}}, nil, opts)
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, `{"answer":"hello"}`, resp.Content)

	var req struct {
		ResponseFormat *struct {
			Type       string `json:"type"`
			JSONSchema *struct {
				Name   string          `json:"name"`
				Strict bool            `json:"strict"`
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	require.NotNil(t, req.ResponseFormat, "request should include response_format")
	require.Equal(t, "json_schema", req.ResponseFormat.Type)
	require.NotNil(t, req.ResponseFormat.JSONSchema)
	require.Equal(t, "test-schema", req.ResponseFormat.JSONSchema.Name)
	require.True(t, req.ResponseFormat.JSONSchema.Strict)
	require.NotEmpty(t, req.ResponseFormat.JSONSchema.Schema)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &parsed))
	require.Equal(t, "hello", parsed["answer"])
}

func TestOpenAIProvider_Chat_WithoutResponseFormat_RequestOmitsResponseFormat(t *testing.T) {
	srv := NewMockLLMServer().ReturnContent("plain text").Start()
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")
	llmBackend := &core.LLMBackend{
		Endpoint:     srv.URL(),
				APIKeyEnvVar: testAPIKeyEnvVar,
	}

	p, err := NewOpenAIProvider("m", llmBackend)
	require.NoError(t, err)

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, InferenceOptions{Stream: false})
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "plain text", resp.Content)

	var req struct {
		ResponseFormat any `json:"response_format"`
	}
	require.NoError(t, json.Unmarshal(srv.LastRequestBody(), &req))
	require.Nil(t, req.ResponseFormat, "request should not include response_format when opts.ResponseFormat is nil")
}
