//go:build integration

package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	orlaTesting "github.com/dorcha-inc/orla/internal/testing"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcollama "github.com/testcontainers/testcontainers-go/modules/ollama"
)

func prettyPrint(t *testing.T, prefix string, v any) {
	pretty, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	t.Logf("%s: %s", prefix, string(pretty))
}

// ensureOllamaAvailable ensures Ollama is available for integration tests
// Returns the provider ready to use, or fails the test with a clear error message
// Note: Ollama must be running before running integration tests
func ensureOllamaAvailable(t *testing.T) *OllamaProvider {
	cfg := &config.OrlaConfig{}
	provider, err := NewOllamaProvider(orlaTesting.GetTestModelName(), cfg)
	require.NoError(t, err, "Failed to create Ollama provider")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ensure Ollama is ready (must be running)
	err = provider.EnsureReady(ctx)
	require.NoError(t, err, "Failed to ensure Ollama is ready. Please ensure Ollama is running and accessible at the configured endpoint")

	// Verify the model is available by attempting a simple request
	// This will fail if the model doesn't exist, giving us a clear error
	testMessages := []Message{
		{Role: MessageRoleUser, Content: "test"},
	}
	_, _, testErr := provider.Chat(ctx, testMessages, nil, false)
	if testErr != nil {
		if strings.Contains(testErr.Error(), "model") && strings.Contains(testErr.Error(), "not found") {
			t.Fatalf("Model '%s' is not available. Please pull it with: ollama pull %s", orlaTesting.GetTestModelName(), orlaTesting.GetTestModelName())
		}
		// Other errors might be transient, but let's fail anyway to be safe
		require.NoError(t, testErr, "Failed to verify model availability")
	}

	return provider
}

// ===== Integration tests - these require Ollama to be available =====
// These tests are only run when the 'integration' build tag is specified.
// Run with: go test -tags=integration ./internal/model
// Or use: make test-integration

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
	thinkingChunks := []string{}
	toolCallEvents := []string{}

	timeout := time.After(5 * time.Minute)
	for {
		select {
		case chunk, ok := <-streamCh:
			if !ok {
				// Channel closed
				t.Logf("Stream closed. Received %d content chunks total", len(chunks))
				goto done
			}
			// Handle different event types
			if contentEvent, ok := chunk.(*ContentEvent); ok {
				chunks = append(chunks, contentEvent.Content)
			} else if thinkingEvent, ok := chunk.(*ThinkingEvent); ok {
				thinkingChunks = append(thinkingChunks, thinkingEvent.Content)
			} else if toolCallEvent, ok := chunk.(*ToolCallEvent); ok {
				toolCallEvents = append(toolCallEvents, toolCallEvent.Name)
			} else {
				t.Logf("Unexpected event type: %s, value: %+v", chunk.Type(), chunk)
			}
		case <-timeout:
			t.Fatalf("Stream timeout after receiving %d content chunks and %d thinking chunks and %d tool call events", len(chunks), len(thinkingChunks), len(toolCallEvents))
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

// TestOllamaProvider_RemoteEndpoint_Integration tests connecting to a remote Ollama instance
// using llm_backend configuration. This test uses testcontainers to spin up an Ollama container
// to simulate a remote endpoint.
//
// Note: This test requires Docker to be running. It will fail if Docker is not available.
func TestOllamaProvider_RemoteEndpoint_Integration(t *testing.T) {
	ctx := context.Background()

	// Start Ollama container using testcontainers
	// Testcontainers will handle Docker availability and provide clear error messages
	ollamaContainer, runErr := tcollama.Run(ctx, "ollama/ollama:latest")
	require.NoError(t, runErr, "Failed to start ollama container")

	defer func() {
		terminateErr := testcontainers.TerminateContainer(ollamaContainer)
		if terminateErr != nil {
			t.Logf("failed to terminate container: %v", terminateErr)
		}
	}()

	// Get the connection string (includes host and port)
	connectionStr, getConnectionStringErr := ollamaContainer.ConnectionString(ctx)
	require.NoError(t, getConnectionStringErr, "Failed to get Ollama connection string")

	// Pull the test model in the container
	modelName := orlaTesting.GetTestModelName()
	_, _, pullErr := ollamaContainer.Exec(ctx, []string{"ollama", "pull", modelName})
	require.NoError(t, pullErr, "Failed to pull model in container")

	// Test with llm_backend configuration pointing to the container
	cfg := &config.OrlaConfig{
		LLMBackend: &core.LLMBackend{
			Endpoint: connectionStr,
			Type:     core.LLMInferenceAPITypeOllama,
		},
	}

	provider, createProviderErr := NewOllamaProvider(modelName, cfg)
	require.NoError(t, createProviderErr, "Failed to create Ollama provider with remote endpoint")

	testCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ensure the remote Ollama is ready
	ensureErr := provider.EnsureReady(testCtx)
	require.NoError(t, ensureErr, "Failed to ensure remote Ollama is ready")

	// Test a simple chat request
	messages := []Message{
		{Role: MessageRoleUser, Content: "Say hello"},
	}

	response, streamCh, chatErr := provider.Chat(testCtx, messages, nil, false)
	require.NoError(t, chatErr)
	assert.NotNil(t, response)
	assert.Nil(t, streamCh) // stream=false
	assert.NotEmpty(t, response.Content)
	t.Logf("Remote Ollama response: %s", response.Content)
}
