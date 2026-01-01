package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testModelName      = "qwen3:0.6b"
	testInvalidBaseURL = "http://localhost:42424" // Used for testing connection failures
)

func prettyPrint(t *testing.T, prefix string, v any) {
	pretty, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	t.Logf("%s: %s", prefix, string(pretty))
}

// ensureOllamaAvailable ensures Ollama is available for integration tests
// It will attempt to start Ollama if it's not running (with auto-start enabled)
// Returns the provider ready to use, or fails the test with a clear error message
func ensureOllamaAvailable(t *testing.T) *OllamaProvider {
	cfg := &config.OrlaConfig{
		AutoStartOllama: true, // Enable auto-start for integration tests
	}
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err, "Failed to create Ollama provider")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ensure Ollama is ready (will start it if needed)
	err = provider.EnsureReady(ctx)
	require.NoError(t, err, "Failed to ensure Ollama is ready. Please install Ollama: https://ollama.ai")

	// Verify the model is available by attempting a simple request
	// This will fail if the model doesn't exist, giving us a clear error
	testMessages := []Message{
		{Role: MessageRoleUser, Content: "test"},
	}
	_, _, testErr := provider.Chat(ctx, testMessages, nil, false)
	if testErr != nil {
		if strings.Contains(testErr.Error(), "model") && strings.Contains(testErr.Error(), "not found") {
			t.Fatalf("Model '%s' is not available. Please pull it with: ollama pull %s", testModelName, testModelName)
		}
		// Other errors might be transient, but let's fail anyway to be safe
		require.NoError(t, testErr, "Failed to verify model availability")
	}

	return provider
}

func TestNewOllamaProvider(t *testing.T) {
	cfg := &config.OrlaConfig{}

	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "ollama", provider.Name())
}

func TestNewOllamaProvider_WithEnvVar(t *testing.T) {
	// Set environment variable
	originalValue := os.Getenv("OLLAMA_HOST")
	defer func() {
		if originalValue != "" {
			require.NoError(t, os.Setenv("OLLAMA_HOST", originalValue))
			return
		}
		require.NoError(t, os.Unsetenv("OLLAMA_HOST"))
	}()

	require.NoError(t, os.Setenv("OLLAMA_HOST", "http://custom:11434"))
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)
	assert.Equal(t, "ollama", provider.Name())
}

func TestOllamaProvider_EnsureReady_NoAutoStart(t *testing.T) {
	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}

	// Create a provider with a baseURL that will fail to connect (simulating Ollama not running)
	// Use a valid port number that's guaranteed not to have Ollama running
	provider := &OllamaProvider{
		modelName: testModelName,
		baseURL:   "http://localhost:42424", // Different port from default 11434
		client:    &http.Client{Timeout: 1 * time.Second},
		cfg:       cfg,
	}

	ctx := context.Background()
	err := provider.EnsureReady(ctx)
	require.Error(t, err)
	// The error should indicate Ollama is not running and auto-start is disabled
	assert.Contains(t, err.Error(), "ollama is not running and auto_start_ollama is disabled")
}

// ===== Integration tests - these require Ollama to be available =====
func TestOllamaProvider_Chat_Integration(t *testing.T) {
	provider := ensureOllamaAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test simple chat
	messages := []Message{
		{Role: MessageRoleUser, Content: "Say hello"},
	}

	t.Logf("Sending Messages: %+v", messages)

	response, streamCh, err := provider.Chat(ctx, messages, nil, false)

	t.Logf("Response: %+v", response)

	require.NoError(t, err)
	assert.NotNil(t, response)
	// streamCh is nil when stream=false
	assert.Nil(t, streamCh)
	assert.NotEmpty(t, response.Content)
}

func TestOllamaProvider_Chat_WithTools_Integration(t *testing.T) {
	provider := ensureOllamaAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a test tool
	tools := []*mcp.Tool{
		{
			Name:        "get_temperature",
			Description: "Get the temperature for a city",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "The name of the city",
					},
				},
				"required": []string{"city"},
			},
		},
	}

	messages := []Message{
		{Role: MessageRoleUser, Content: "What is the temperature in Boston?"},
	}

	prettyPrint(t, "Sending Messages", messages)

	response, streamCh, err := provider.Chat(ctx, messages, tools, false)
	require.NoError(t, err)

	prettyPrint(t, "Response", response)

	require.NoError(t, err)
	assert.NotNil(t, response)
	// streamCh is nil when stream=false
	assert.Nil(t, streamCh)

	require.NotNil(t, response.ToolCalls)
	require.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "get_temperature", response.ToolCalls[0].McpCallToolParams.Name)
	assert.Equal(t, map[string]any{"city": "Boston"}, response.ToolCalls[0].McpCallToolParams.Arguments)

}

func TestOllamaProvider_Chat_Streaming_Integration(t *testing.T) {
	provider := ensureOllamaAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []Message{
		{Role: MessageRoleUser, Content: "Count to 5"},
	}

	prettyPrint(t, "Sending Messages", messages)

	response, streamCh, err := provider.Chat(ctx, messages, nil, true)
	require.NoError(t, err)
	// Streaming responses return a response object that gets populated as chunks arrive
	// The response is shared between the goroutine (writer) and caller (reader)
	assert.NotNil(t, response)
	assert.NotNil(t, streamCh)

	// Read from stream
	chunks := []string{}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case chunk, ok := <-streamCh:
			if !ok {
				// Channel closed
				t.Logf("Stream closed. Received %d chunks total", len(chunks))
				goto done
			}
			chunks = append(chunks, chunk)
			t.Logf("Stream chunk [%d]: %s", len(chunks), chunk)
		case <-timeout:
			t.Fatalf("Stream timeout after receiving %d chunks", len(chunks))
		}
	}

done:
	assert.NotEmpty(t, chunks, "Should receive at least one chunk")
	t.Logf("Total streamed content: %s", strings.Join(chunks, ""))

	// After stream completes, response should be populated with the accumulated content
	// The channel close provides synchronization, so response.Content should be available now
	assert.NotNil(t, response)
	assert.Equal(t, strings.Join(chunks, ""), response.Content, "Response content should match streamed chunks")
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
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)

	originalTimeout := provider.client.Timeout
	newTimeout := 30 * time.Second
	provider.SetTimeout(newTimeout)
	assert.Equal(t, newTimeout, provider.client.Timeout)
	assert.NotEqual(t, originalTimeout, provider.client.Timeout)
}

func TestOllamaProvider_EnsureReady_NotInstalled(t *testing.T) {
	// Create a provider with a custom baseURL that won't exist
	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)

	// Override the baseURL to point to a non-existent host
	provider.baseURL = testInvalidBaseURL

	ctx := context.Background()
	err = provider.EnsureReady(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto_start_ollama is disabled")
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
	provider, err := NewOllamaProvider(testModelName, cfg)
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
	provider, err := NewOllamaProvider(testModelName, cfg)
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
	provider, err := NewOllamaProvider(testModelName, cfg)
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
	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)

	// Use a baseURL that will cause connection errors
	provider.baseURL = testInvalidBaseURL

	ctx := context.Background()
	err = provider.EnsureReady(ctx)
	// Should return error about auto-start being disabled (since isRunning will return false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auto_start_ollama is disabled")
}

func TestOllamaProvider_Chat_EnsureReadyFails(t *testing.T) {
	// Test Chat when EnsureReady fails
	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}
	provider, err := NewOllamaProvider(testModelName, cfg)
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
	assert.Contains(t, err.Error(), "auto_start_ollama is disabled")
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

	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}
	provider := &OllamaProvider{
		modelName: testModelName,
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

	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}
	provider := &OllamaProvider{
		modelName: testModelName,
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

	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}
	provider := &OllamaProvider{
		modelName: testModelName,
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

func TestOllamaProvider_startOllama_NotInstalled(t *testing.T) {
	// Test startOllama when ollama command is not found
	// This tests the error path when exec.LookPath fails
	cfg := &config.OrlaConfig{
		AutoStartOllama: true,
	}
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)

	// Temporarily modify PATH to exclude ollama
	originalPath := os.Getenv("PATH")
	defer func() {
		if originalPath != "" {
			require.NoError(t, os.Setenv("PATH", originalPath))
		}
	}()

	// Set PATH to empty to make LookPath fail
	require.NoError(t, os.Setenv("PATH", ""))

	ctx := context.Background()
	err = provider.startOllama(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama command not found")
}

func TestOllamaProvider_EnsureReady_StartOllamaFails(t *testing.T) {
	// Test EnsureReady when startOllama fails (command not found)
	// Note: isRunning() will also fail when PATH is empty, so we get that error first
	cfg := &config.OrlaConfig{
		AutoStartOllama: true,
	}
	provider, err := NewOllamaProvider(testModelName, cfg)
	require.NoError(t, err)

	// Use a baseURL that will make isRunning return false
	provider.baseURL = testInvalidBaseURL

	// Temporarily modify PATH to exclude ollama
	originalPath := os.Getenv("PATH")
	defer func() {
		if originalPath != "" {
			require.NoError(t, os.Setenv("PATH", originalPath))
		}
	}()

	// Set PATH to empty to make LookPath fail
	require.NoError(t, os.Setenv("PATH", ""))

	ctx := context.Background()
	err = provider.EnsureReady(ctx)
	require.Error(t, err)
	// When PATH is empty, isRunning() will fail first, so we get that error
	// The error message will be about ollama not being installed
	assert.Contains(t, err.Error(), "ollama")
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

	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}

	// Create provider with custom base URL
	provider := &OllamaProvider{
		modelName: testModelName,
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

	cfg := &config.OrlaConfig{
		AutoStartOllama: false,
	}

	provider := &OllamaProvider{
		modelName: testModelName,
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
