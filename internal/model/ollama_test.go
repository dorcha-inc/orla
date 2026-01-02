package model

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	orlaTesting "github.com/dorcha-inc/orla/internal/testing"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testInvalidBaseURL = "http://localhost:42424" // Used for testing connection failures
)

func TestNewOllamaProvider(t *testing.T) {
	cfg := &config.OrlaConfig{}

	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "ollama", provider.Name())
}

func TestNewOllamaProvider_WithEnvVar(t *testing.T) {
	// Set environment variable
	t.Setenv("OLLAMA_HOST", "http://custom:11434")
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "ollama", provider.Name())
}

func TestOllamaProvider_EnsureReady_NotRunning(t *testing.T) {
	cfg := &config.OrlaConfig{}

	// Create a provider with a baseURL that will fail to connect (simulating Ollama not running)
	// Use a valid port number that's guaranteed not to have Ollama running
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   "http://localhost:42424", // Different port from default 11434
		client:    &http.Client{Timeout: 1 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	err := provider.EnsureReady(ctx)
	require.Error(t, err)
	// The error should indicate Ollama is not running with instructions to start it
	assert.Contains(t, err.Error(), "ollama is not running")
	assert.Contains(t, err.Error(), "brew services start ollama")
}

// Test helper functions
func TestConvertToolsToOllamaFormat(t *testing.T) {

	tools := []*mcp.Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"arg1": map[string]any{"type": "string"},
				},
			},
		},
	}

	ollamaTools := convertToolsToOllamaFormat(tools)
	require.Len(t, ollamaTools, 1)
	assert.Equal(t, "function", ollamaTools[0].Type)
	assert.Equal(t, "test_tool", ollamaTools[0].Function.Name)
	assert.Equal(t, "A test tool", ollamaTools[0].Function.Description)
	assert.NotNil(t, ollamaTools[0].Function.Parameters)
}

func TestConvertOllamaToolCalls(t *testing.T) {
	// Test with object arguments
	ollamaCalls := []ollamaToolCall{
		{
			Type: "function",
			Function: ollamaToolCallFunction{
				Index:     intPtr(0),
				Name:      "test_tool",
				Arguments: map[string]any{"arg1": "value1"},
			},
		},
	}

	toolCalls := convertOllamaToolCalls(ollamaCalls)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_0", toolCalls[0].ID)
	assert.Equal(t, "test_tool", toolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, map[string]any{"arg1": "value1"}, toolCalls[0].McpCallToolParams.Arguments)
}

func TestConvertOllamaToolCalls_StringArguments(t *testing.T) {
	// Test with JSON string arguments
	ollamaCalls := []ollamaToolCall{
		{
			Type: "function",
			Function: ollamaToolCallFunction{
				Name:      "test_tool",
				Arguments: `{"arg1":"value1"}`,
			},
		},
	}

	toolCalls := convertOllamaToolCalls(ollamaCalls)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "test_tool", toolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, map[string]any{"arg1": "value1"}, toolCalls[0].McpCallToolParams.Arguments)
}

// Helper function
func intPtr(i int) *int {
	return &i
}

func TestOllamaProvider_SetTimeout(t *testing.T) {
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)

	originalTimeout := provider.client.Timeout
	newTimeout := 30 * time.Second
	provider.SetTimeout(newTimeout)
	assert.Equal(t, newTimeout, provider.client.Timeout)
	assert.NotEqual(t, originalTimeout, provider.client.Timeout)
}

func TestOllamaProvider_EnsureReady_NotInstalled(t *testing.T) {
	// Create a provider with a custom baseURL that won't exist
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)

	// Override the baseURL to point to a non-existent host
	provider.baseURL = testInvalidBaseURL

	ctx := context.Background()
	err = provider.EnsureReady(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama is not running")
	assert.Contains(t, err.Error(), "brew services start ollama")
}

func TestOllamaProvider_waitForReady_Timeout(t *testing.T) {
	// Create a mock server that never responds to health checks
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			// Return 503 to simulate not ready
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)
	provider.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Use a very short timeout to test timeout behavior
	// The timeout should be shorter than the context timeout to trigger the timeout error
	err = provider.waitForReady(ctx, 50*time.Millisecond)
	require.Error(t, err)
	// The error could be either "timeout waiting for Ollama" or context deadline exceeded
	// depending on which happens first
	assert.True(t, err.Error() == "timeout waiting for Ollama to become ready" || err.Error() == "context deadline exceeded",
		"Expected timeout error, got: %v", err)
}

func TestOllamaProvider_waitForReady_Success(t *testing.T) {
	// Create a mock server that responds to health checks
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)
	provider.baseURL = server.URL

	ctx := context.Background()
	err = provider.waitForReady(ctx, 5*time.Second)
	require.NoError(t, err)
}

func TestOllamaProvider_waitForReady_ContextCancelled(t *testing.T) {
	// Create a mock server that never responds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)
	provider.baseURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = provider.waitForReady(ctx, 5*time.Second)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestConvertOllamaToolCalls_StringJSON(t *testing.T) {
	// Test with string JSON arguments
	ollamaCalls := []ollamaToolCall{
		{
			Type: "function",
			Function: ollamaToolCallFunction{
				Name:      "test_tool",
				Arguments: `{"arg1": "value1", "arg2": 42}`,
			},
		},
	}

	toolCalls := convertOllamaToolCalls(ollamaCalls)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "test_tool", toolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, map[string]any{"arg1": "value1", "arg2": float64(42)}, toolCalls[0].McpCallToolParams.Arguments)
}

func TestConvertOllamaToolCalls_InvalidJSONString(t *testing.T) {
	// Test with invalid JSON string
	ollamaCalls := []ollamaToolCall{
		{
			Type: "function",
			Function: ollamaToolCallFunction{
				Name:      "test_tool",
				Arguments: `{"arg1": invalid json}`,
			},
		},
	}

	toolCalls := convertOllamaToolCalls(ollamaCalls)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "test_tool", toolCalls[0].McpCallToolParams.Name)
	// Should fall back to empty map on parse error
	assert.Equal(t, map[string]any{}, toolCalls[0].McpCallToolParams.Arguments)
}

func TestConvertOllamaToolCalls_DefaultCase(t *testing.T) {
	// Test with a type that needs marshal/unmarshal (e.g., slice)
	ollamaCalls := []ollamaToolCall{
		{
			Type: "function",
			Function: ollamaToolCallFunction{
				Name:      "test_tool",
				Arguments: []interface{}{"item1", "item2"},
			},
		},
	}

	toolCalls := convertOllamaToolCalls(ollamaCalls)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "test_tool", toolCalls[0].McpCallToolParams.Name)
	// Should handle the default case and convert to map
	assert.NotNil(t, toolCalls[0].McpCallToolParams.Arguments)
}

func TestConvertToolsToOllamaFormat_WithSchema(t *testing.T) {

	tools := []*mcp.Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"arg1": map[string]any{"type": "string"},
				},
			},
		},
	}

	ollamaTools := convertToolsToOllamaFormat(tools)
	require.Len(t, ollamaTools, 1)
	assert.Equal(t, "function", ollamaTools[0].Type)
	assert.Equal(t, "test_tool", ollamaTools[0].Function.Name)
	assert.Equal(t, "A test tool", ollamaTools[0].Function.Description)
	assert.NotNil(t, ollamaTools[0].Function.Parameters)
}

func TestConvertToolsToOllamaFormat_WithoutSchema(t *testing.T) {

	tools := []*mcp.Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: nil,
		},
	}

	ollamaTools := convertToolsToOllamaFormat(tools)
	require.Len(t, ollamaTools, 1)
	assert.Equal(t, "function", ollamaTools[0].Type)
	assert.Equal(t, "test_tool", ollamaTools[0].Function.Name)
	assert.Equal(t, map[string]any{}, ollamaTools[0].Function.Parameters)
}

func TestConvertToolsToOllamaFormat_InvalidSchema(t *testing.T) {
	// Test with InputSchema that's not a map[string]any

	tools := []*mcp.Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: "not a map",
		},
	}

	ollamaTools := convertToolsToOllamaFormat(tools)
	require.Len(t, ollamaTools, 1)
	assert.Equal(t, "function", ollamaTools[0].Type)
	assert.Equal(t, "test_tool", ollamaTools[0].Function.Name)
	// Should fall back to empty map
	assert.Equal(t, map[string]any{}, ollamaTools[0].Function.Parameters)
}

func TestOllamaProvider_EnsureReady_IsRunningError(t *testing.T) {
	// Test EnsureReady when isRunning returns a non-ErrOllamaNotInstalled error
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)

	// Use a baseURL that will cause connection errors
	provider.baseURL = testInvalidBaseURL

	ctx := context.Background()
	err = provider.EnsureReady(ctx)
	// Should return error about Ollama not running
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama is not running")
	assert.Contains(t, err.Error(), "brew services start ollama")
}

func TestOllamaProvider_Chat_EnsureReadyFails(t *testing.T) {
	// Test Chat when EnsureReady fails
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err)

	// Use a baseURL that will make isRunning return false
	provider.baseURL = testInvalidBaseURL

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "test"},
	}

	response, streamCh, err := provider.Chat(ctx, messages, nil, false)
	require.Error(t, err)
	assert.Nil(t, response)
	assert.Nil(t, streamCh)
	assert.Contains(t, err.Error(), "ollama is not running")
}

func TestOllamaProvider_Chat_HTTPError(t *testing.T) {
	// Test Chat when HTTP request fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == ollamaChatEndpoint {
			// Return an error status
			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte("Internal Server Error"))
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "test"},
	}

	response, streamCh, err := provider.Chat(ctx, messages, nil, false)
	require.Error(t, err)
	assert.Nil(t, response)
	assert.Nil(t, streamCh)
	assert.Contains(t, err.Error(), "ollama API error: 500")
}

func TestOllamaProvider_Chat_DecodeError(t *testing.T) {
	// Test Chat when JSON decode fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == ollamaChatEndpoint {
			// Return invalid JSON
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte("invalid json{"))
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "test"},
	}

	response, streamCh, err := provider.Chat(ctx, messages, nil, false)
	require.Error(t, err)
	assert.Nil(t, response)
	assert.Nil(t, streamCh)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestOllamaProvider_Chat_WithTools_NoToolCalls(t *testing.T) {
	// Test Chat with tools but no tool calls in response (content path)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == ollamaChatEndpoint {
			// Return response with content but no tool_calls
			response := `{
				"message": {
					"role": "assistant",
					"content": "I don't need to call any tools for this."
				},
				"done": true
			}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(response))
			require.NoError(t, err)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	tools := []*mcp.Tool{
		{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: map[string]any{
				"type": "object",
			},
		},
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "test"},
	}

	response, streamCh, err := provider.Chat(ctx, messages, tools, false)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Nil(t, streamCh)
	assert.Empty(t, response.ToolCalls)  // No tool calls
	assert.NotEmpty(t, response.Content) // But has content
}

// Test with mock HTTP server
func TestOllamaProvider_Chat_Mock(t *testing.T) {
	// Create a mock Ollama server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			// Health check endpoint
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != ollamaChatEndpoint {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Return a mock response
		response := `{
			"message": {
				"role": "assistant",
				"content": "Hello, world!"
			},
			"done": true
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(response))
		if err != nil {
			t.Logf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}

	// Create provider with custom base URL
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "Hello"},
	}

	response, streamCh, err := provider.Chat(ctx, messages, nil, false)
	require.NoError(t, err)
	assert.NotNil(t, response)
	// streamCh is nil when stream=false
	assert.Nil(t, streamCh)
	assert.Equal(t, "Hello, world!", response.Content)
}

func TestOllamaProvider_Chat_Mock_WithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			// Health check endpoint
			w.WriteHeader(http.StatusOK)
			return
		}
		response := `{
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{
						"type": "function",
						"function": {
							"index": 0,
							"name": "get_temperature",
							"arguments": {"city": "Boston"}
						}
					}
				]
			},
			"done": true
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(response))
		if err != nil {
			t.Logf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}

	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "What's the temperature?"},
	}

	tools := []*mcp.Tool{
		{
			Name:        "get_temperature",
			Description: "Get temperature",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
			},
		},
	}

	response, _, err := provider.Chat(ctx, messages, tools, false)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "get_temperature", response.ToolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, map[string]any{"city": "Boston"}, response.ToolCalls[0].McpCallToolParams.Arguments)
}

func TestOllamaProvider_Chat_Stream_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Simulate a streaming response
		w.Header().Set("Content-Type", "application/json")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.ResponseWriter to be an http.Flusher")
		}

		// Stream chunks
		chunks := []string{
			`{"message": {"role": "assistant", "content": "Hello, "}, "done": false}`,
			`{"message": {"role": "assistant", "content": "world!"}, "done": false}`,
			`{"message": {"role": "assistant", "tool_calls": [{"type": "function", "function": {"name": "test_tool", "arguments": {}}}]}}, "done": false}`,
			`{"done": true}`,
		}

		for _, chunk := range chunks {
			_, err := w.Write([]byte(chunk + "\n"))
			require.NoError(t, err)
			flusher.Flush()
		}
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleUser, Content: "Hello"},
	}

	response, streamCh, err := provider.Chat(ctx, messages, nil, true)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, streamCh)

	// Consume stream
	var contentEvents int
	var toolCallEvents int
	var accumulatedContent string
	for event := range streamCh {
		switch e := event.(type) {
		case *ContentEvent:
			contentEvents++
			accumulatedContent += e.Content
		case *ToolCallEvent:
			toolCallEvents++
			assert.Equal(t, "test_tool", e.Name)
		}
	}

	assert.Equal(t, 2, contentEvents)
	assert.Equal(t, 1, toolCallEvents)
	assert.Equal(t, "Hello, world!", accumulatedContent)
	assert.Equal(t, "Hello, world!", response.Content)
	assert.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "test_tool", response.ToolCalls[0].McpCallToolParams.Name)
}

func TestGetArgumentsForToolCall_InvalidJSON(t *testing.T) {
	toolCall := ollamaToolCall{
		Function: ollamaToolCallFunction{
			Arguments: `{"bad": json`,
		},
	}
	args, err := getArgumentsForToolCall(toolCall)
	assert.Error(t, err)
	assert.Nil(t, args)
}

func TestOllamaProvider_Chat_ThinkEnabled_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		var reqBody ollamaChatRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)
		assert.True(t, reqBody.Think)

		response := `{
			"message": {
				"role": "assistant",
				"content": "Hello",
				"thinking": "I am thinking."
			},
			"done": true
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte(response))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{ShowThinking: true}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{{Role: MessageRoleUser, Content: "Hello"}}

	response, _, err := provider.Chat(ctx, messages, nil, false)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "I am thinking.", response.Thinking)
}

func TestOllamaProvider_Chat_WithToolMessage_Mock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ollamaHealthCheckEndpoint {
			w.WriteHeader(http.StatusOK)
			return
		}
		var reqBody ollamaChatRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)
		require.Len(t, reqBody.Messages, 1)
		assert.Equal(t, "tool", reqBody.Messages[0].Role)
		assert.Equal(t, "test_tool", reqBody.Messages[0].ToolName)

		response := `{"message": {"role": "assistant", "content": "OK"}, "done": true}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte(response))
		require.NoError(t, err)
	}))
	defer server.Close()

	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Timeout: 5 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	messages := []Message{
		{Role: MessageRoleTool, Content: "tool output", ToolName: "test_tool"},
	}

	_, _, err := provider.Chat(ctx, messages, nil, false)
	require.NoError(t, err)
}

func TestOllamaProvider_Chat_NewRequestError(t *testing.T) {
	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   "http://invalid-url-with-spaces.com/ a",
		client:    &http.Client{Timeout: 1 * time.Second},
		cfg:       cfg,
	}
	ctx := context.Background()
	messages := []Message{{Role: MessageRoleUser, Content: "Hello"}}
	_, _, err := provider.Chat(ctx, messages, nil, false)
	assert.Error(t, err)
}

type errorRoundTripper struct{}

func (e *errorRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, assert.AnError
}

func TestOllamaProvider_Chat_ClientDoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := &config.OrlaConfig{}
	provider := &OllamaProvider{
		modelName: orlaTesting.GetTestModelName(),
		baseURL:   server.URL,
		client:    &http.Client{Transport: &errorRoundTripper{}},
		cfg:       cfg,
	}
	ctx := context.Background()
	messages := []Message{{Role: MessageRoleUser, Content: "Hello"}}
	_, _, err := provider.Chat(ctx, messages, nil, false)
	assert.Error(t, err)
}

func TestIsRunning_NewRequestError(t *testing.T) {
	provider := &OllamaProvider{
		baseURL: "http://localhost:11434/\x7f",
	}
	_, err := provider.isRunning()
	assert.Error(t, err)
}

func TestHandleStreamResponse_DecodeError(t *testing.T) {
	body := io.NopCloser(strings.NewReader("invalid-json\n"))
	_, streamCh := (&OllamaProvider{}).handleStreamResponse(body)
	for range streamCh {
	}
	// No assertions needed, just make sure it doesn't panic
}

func TestConvertOllamaToolCalls_MarshalError(t *testing.T) {
	ollamaCalls := []ollamaToolCall{
		{
			Type: "function",
			Function: ollamaToolCallFunction{
				Name:      "test_tool",
				Arguments: func() {}, // Functions cannot be marshalled to JSON
			},
		},
	}
	toolCalls := convertOllamaToolCalls(ollamaCalls)
	assert.Len(t, toolCalls, 1)
	assert.Equal(t, "test_tool", toolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, map[string]any{}, toolCalls[0].McpCallToolParams.Arguments)
}

func TestGetArgumentsForToolCall_MapArgs(t *testing.T) {
	tc := ollamaToolCall{
		Function: ollamaToolCallFunction{
			Arguments: map[string]any{"a": "b"},
		},
	}

	args, err := getArgumentsForToolCall(tc)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": "b"}, args)
}

func TestGetArgumentsForToolCall_StringArgs(t *testing.T) {
	tc := ollamaToolCall{
		Function: ollamaToolCallFunction{
			Arguments: `{"x": 1}`,
		},
	}

	args, err := getArgumentsForToolCall(tc)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"x": float64(1)}, args)
}

func TestGetArgumentsForToolCall_InvalidString(t *testing.T) {
	tc := ollamaToolCall{
		Function: ollamaToolCallFunction{
			Arguments: `{invalid json}`,
		},
	}

	_, err := getArgumentsForToolCall(tc)
	require.Error(t, err)
}

func TestHandleStreamResponse_AccumulationAndEvents(t *testing.T) {
	// Create three JSON chunks: content, thinking+content, tool_call+done
	chunk1 := `{ "message": { "content": "Hello", "role": "assistant" }, "done": false }`
	chunk2 := `{ "message": { "thinking": "[t]", "content": " world", "role": "assistant" }, "done": false }`
	chunk3 := `{
        "message": {
            "content": "",
            "role": "assistant",
            "tool_calls": [
                { "type": "function", "function": { "index": 0, "name": "do_it", "arguments": {"param":"val"} } }
            ]
        },
        "done": true
    }`

	// Concatenate chunks without separators to simulate streaming JSON objects
	streamData := chunk1 + chunk2 + chunk3

	rc := io.NopCloser(bytes.NewBufferString(streamData))

	p := &OllamaProvider{}
	resp, ch := p.handleStreamResponse(rc)

	// Consume events until channel closes
	var contentBuf string
	var thinkingBuf string
	var toolCallEvents int

	for ev := range ch {
		switch e := ev.(type) {
		case *ContentEvent:
			contentBuf += e.Content
		case *ThinkingEvent:
			thinkingBuf += e.Content
		case *ToolCallEvent:
			toolCallEvents++
			// ensure argument parsed
			if v, ok := e.Arguments["param"]; ok {
				assert.Equal(t, "val", v)
			}
		default:
			t.Fatalf("unexpected event type: %T", ev)
		}
	}

	// After channel close, response should have accumulated content and tool calls
	assert.Equal(t, "Hello world", contentBuf)
	assert.Equal(t, "[t]", thinkingBuf)
	// Tool calls are populated after stream completes
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "do_it", resp.ToolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, 1, toolCallEvents)
}
