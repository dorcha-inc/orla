package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/require"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
)

func TestNormalizeSchemaToMap_MapPassthrough(t *testing.T) {
	t.Parallel()

	tool := &mcp.Tool{
		Name:        "hello",
		Description: "desc",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
	}

	got, err := normalizeSchemaToMap(tool)
	require.NoError(t, err)
	require.Equal(t, "object", got["type"])
}

func TestNormalizeSchemaToMap_RawMessage(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)
	tool := &mcp.Tool{Name: "raw", InputSchema: raw}

	got, err := normalizeSchemaToMap(tool)
	require.NoError(t, err)
	require.Equal(t, "object", got["type"])
}

func TestNormalizeSchemaToMap_JSONBytes(t *testing.T) {
	t.Parallel()

	b := []byte(`{"type":"object"}`)
	tool := &mcp.Tool{Name: "bytes", InputSchema: b}

	got, err := normalizeSchemaToMap(tool)
	require.NoError(t, err)
	require.Equal(t, "object", got["type"])
}

func TestNormalizeSchemaToMap_MarshalableStruct(t *testing.T) {
	t.Parallel()

	type schema struct {
		Type string `json:"type"`
	}
	tool := &mcp.Tool{Name: "struct", InputSchema: schema{Type: "object"}}

	got, err := normalizeSchemaToMap(tool)
	require.NoError(t, err)
	require.Equal(t, "object", got["type"])
}

func TestNormalizeSchemaToMap_NilSchema(t *testing.T) {
	t.Parallel()

	tool := &mcp.Tool{Name: "nil", InputSchema: nil}
	_, err := normalizeSchemaToMap(tool)
	require.Error(t, err)
}

func TestConvertToolsToOpenAIFormat_UsesNormalizedSchema(t *testing.T) {
	t.Parallel()

	tools := []*mcp.Tool{
		{
			Name:        "t1",
			Description: "d1",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
		},
	}

	got, err := convertToolsToOpenAIFormat(tools)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, openai.ToolTypeFunction, got[0].Type)
	require.NotNil(t, got[0].Function)
	require.Equal(t, "t1", got[0].Function.Name)

	// Parameters should be a map[string]any after normalization.
	_, ok := got[0].Function.Parameters.(map[string]any)
	require.True(t, ok, "expected parameters to be map[string]any, got %T", got[0].Function.Parameters)
}

func TestConvertMessageToOpenAI_ToolRoleSetsToolCallID(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:       MessageRoleTool,
		Content:    "result",
		ToolName:   "some_tool",
		ToolCallID: "call_123",
	}

	got := convertMessageToOpenAI(msg)
	require.Equal(t, openai.ChatMessageRoleTool, got.Role)
	require.Equal(t, "call_123", got.ToolCallID)
}

func TestConvertOpenAIToolCalls_ParsesArgumentsAndUsesCallID(t *testing.T) {
	t.Parallel()

	calls := []openai.ToolCall{
		{
			ID:   "abc",
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      "do",
				Arguments: `{"x":"y"}`,
			},
		},
	}

	got, err := convertOpenAIToolCalls(calls)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "abc", got[0].ID)
	require.Equal(t, "do", got[0].McpCallToolParams.Name)

	args, ok := got[0].McpCallToolParams.Arguments.(map[string]any)
	require.True(t, ok, "expected arguments to be map[string]any, got %T", got[0].McpCallToolParams.Arguments)
	require.Equal(t, "y", args["x"])
}

func TestConvertOpenAIToolCallsFromStream_BestEffortSkipsBadJSON(t *testing.T) {
	t.Parallel()

	calls := []openai.ToolCall{
		{
			ID:   "good",
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      "good_tool",
				Arguments: `{"ok":true}`,
			},
		},
		{
			ID:   "bad",
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      "bad_tool",
				Arguments: `{"oops":`, // invalid JSON
			},
		},
	}

	got, err := convertOpenAIToolCallsFromStream(calls)
	require.Error(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "good", got[0].ID)
}

func TestConvertOpenAIToolCallsFromStream_FallbackIDUsesIndex(t *testing.T) {
	t.Parallel()

	calls := []openai.ToolCall{
		{
			ID:   "",
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      "tool",
				Arguments: `{}`,
			},
		},
	}

	got, err := convertOpenAIToolCallsFromStream(calls)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "call_0", got[0].ID)
}

func TestGetOpenAICompatibleEndpoint_Validation(t *testing.T) {
	t.Parallel()

	_, _, err := getOpenAICompatibleEndpoint(nil)
	require.Error(t, err)

	_, _, err = getOpenAICompatibleEndpoint(&config.OrlaConfig{LLMBackend: nil})
	require.Error(t, err)

	_, _, err = getOpenAICompatibleEndpoint(&config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint: "http://example",
			Type:     "",
		},
	})
	require.Error(t, err)

	_, _, err = getOpenAICompatibleEndpoint(&config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint: "http://example",
			Type:     core.LLMInferenceAPITypeOllama,
		},
	})
	require.Error(t, err)
}

func TestNewOpenAIProvider_RequiresAPIKeyEnvVarValue(t *testing.T) {
	t.Parallel()

	// API key env var is not set => should error.
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     "http://example",
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	_, err := NewOpenAIProvider("model", cfg)
	require.Error(t, err)
}

func TestOpenAIProvider_Chat_NonStreaming_BasicAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "), "expected Bearer auth header")

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
  "id":"cmpl_1",
  "object":"chat.completion",
  "created":0,
  "model":"x",
  "choices":[
    {
      "index":0,
      "message":{
        "role":"assistant",
        "content":"hello",
        "tool_calls":[
          {"id":"call_abc","type":"function","function":{"name":"do","arguments":"{\"x\":\"y\"}"}}
        ]
      },
      "finish_reason":"stop"
    }
  ]
}`))
		require.NoError(t, err)
	}))

	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("m", cfg)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, false, nil)
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "hello", resp.Content)
	require.Len(t, resp.ToolCalls, 1)
	require.Equal(t, "call_abc", resp.ToolCalls[0].ID)
	require.Equal(t, "do", resp.ToolCalls[0].McpCallToolParams.Name)
}

func TestOpenAIProvider_Chat_Streaming_Content(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "POST", r.Method)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Minimal SSE stream that go-openai can parse.
		_, err := w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"he\"}}]}\n\n"))
		require.NoError(t, err)
		_, err = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}\n\n"))
		require.NoError(t, err)
		_, err = w.Write([]byte("data: [DONE]\n\n"))
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("m", cfg)
	require.NoError(t, err)

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, true, nil)
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "POST", r.Method)

		var req openai.ChatCompletionRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify that MaxTokens is set correctly
		require.Equal(t, maxTokens, req.MaxTokens)

		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message: openai.ChatCompletionMessage{
						Role:    "assistant",
						Content: "Short response",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(resp)
		require.NoError(t, err)
	}))
	defer srv.Close()

	t.Setenv("ORLA_TEST_OPENAI_KEY", "test-key")
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("test-model", cfg)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, false, &maxTokens)
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "Short response", resp.Content)
}

func TestOpenAIProvider_Chat_WithoutMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "POST", r.Method)

		var req openai.ChatCompletionRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify that MaxTokens is 0 (default/not set) when maxTokens is nil
		require.Equal(t, 0, req.MaxTokens)

		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message: openai.ChatCompletionMessage{
						Role:    "assistant",
						Content: "Response",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(resp)
		require.NoError(t, err)
	}))
	defer srv.Close()

	t.Setenv("ORLA_TEST_OPENAI_KEY", "test-key")
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("test-model", cfg)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, false, nil)
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
	require.Equal(t, "Response", resp.Content)
}

func TestOpenAIProvider_Chat_WithMaxTokensZero(t *testing.T) {
	maxTokens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "POST", r.Method)

		var req openai.ChatCompletionRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify that MaxTokens is set even when 0
		require.Equal(t, 0, req.MaxTokens)

		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message: openai.ChatCompletionMessage{
						Role:    "assistant",
						Content: "",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(resp)
		require.NoError(t, err)
	}))
	defer srv.Close()

	t.Setenv("ORLA_TEST_OPENAI_KEY", "test-key")
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("test-model", cfg)
	require.NoError(t, err)
	require.NoError(t, p.EnsureReady(context.Background()))

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, false, &maxTokens)
	require.NoError(t, err)
	require.Nil(t, ch)
	require.NotNil(t, resp)
}

func TestOpenAIProvider_Chat_Streaming_WithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "POST", r.Method)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Stream with tool calls
		_, err := w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		require.NoError(t, err)
		_, err = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"tool\",\"arguments\":\"{\\\"x\\\":\\\"y\\\"}\"}}]}}]}\n\n"))
		require.NoError(t, err)
		_, err = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		require.NoError(t, err)
		_, err = w.Write([]byte("data: [DONE]\n\n"))
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")

	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("m", cfg)
	require.NoError(t, err)

	resp, ch, err := p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, true, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, ch)

	// Drain channel
	for range ch {
	}

	require.Equal(t, "hi", resp.Content)
	require.Len(t, resp.ToolCalls, 1)
	require.Equal(t, "call_1", resp.ToolCalls[0].ID)
	require.Equal(t, "tool", resp.ToolCalls[0].McpCallToolParams.Name)
}

func TestConvertMessageToOpenAI_ToolRoleMissingToolCallID(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:     MessageRoleTool,
		Content:  "result",
		ToolName: "some_tool",
		// ToolCallID is empty
	}

	got := convertMessageToOpenAI(msg)
	require.Equal(t, openai.ChatMessageRoleTool, got.Role)
	require.Empty(t, got.ToolCallID)
}

func TestConvertMessageToOpenAI_NonToolRole(t *testing.T) {
	t.Parallel()

	msg := Message{
		Role:    MessageRoleUser,
		Content: "hello",
	}

	got := convertMessageToOpenAI(msg)
	require.Equal(t, openai.ChatMessageRoleUser, got.Role)
	require.Equal(t, "hello", got.Content)
}

func TestOpenAIProvider_Chat_NonStreaming_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"x","object":"chat.completion","created":0,"model":"m","choices":[]}`))
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")

	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     srv.URL,
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("m", cfg)
	require.NoError(t, err)

	_, _, err = p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, nil, false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no choices")
}

func TestOpenAIProvider_Chat_ToolConversionError(t *testing.T) {
	t.Setenv("ORLA_TEST_OPENAI_KEY", "k")

	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint:     "http://example",
			Type:         core.LLMInferenceAPITypeOpenAI,
			APIKeyEnvVar: "ORLA_TEST_OPENAI_KEY",
		},
	}

	p, err := NewOpenAIProvider("m", cfg)
	require.NoError(t, err)

	// Tool with nil InputSchema should cause conversion error
	tools := []*mcp.Tool{
		{
			Name:        "bad_tool",
			Description: "desc",
			InputSchema: nil,
		},
	}

	_, _, err = p.Chat(context.Background(), []Message{{Role: MessageRoleUser, Content: "hi"}}, tools, false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "normalize tool input schema")
}

func TestGetOpenAICompatibleEndpoint_MissingEndpoint(t *testing.T) {
	t.Parallel()

	_, _, err := getOpenAICompatibleEndpoint(&config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint: "",
			Type:     core.LLMInferenceAPITypeOpenAI,
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint is required")
}

func TestNormalizeSchemaToMap_UnmarshalError(t *testing.T) {
	t.Parallel()

	tool := &mcp.Tool{
		Name:        "bad",
		InputSchema: []byte(`{invalid json}`),
	}

	_, err := normalizeSchemaToMap(tool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestNormalizeSchemaToMap_MarshalError(t *testing.T) {
	t.Parallel()

	// Channel cannot be marshaled to JSON
	tool := &mcp.Tool{
		Name:        "bad",
		InputSchema: make(chan int),
	}

	_, err := normalizeSchemaToMap(tool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "marshal")
}

func TestConvertOpenAIToolCalls_ErrorPropagates(t *testing.T) {
	t.Parallel()

	calls := []openai.ToolCall{
		{
			ID:   "bad",
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      "tool",
				Arguments: `{invalid json}`,
			},
		},
	}

	_, err := convertOpenAIToolCalls(calls)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}
