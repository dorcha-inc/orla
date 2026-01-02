package agent

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a mock implementation of model.Provider for testing
type mockProvider struct {
	name            string
	chatFunc        func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error)
	ensureReadyFunc func(ctx context.Context) error
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Chat(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, messages, tools, stream)
	}
	return &model.Response{Content: "test response"}, nil, nil
}

func (m *mockProvider) EnsureReady(ctx context.Context) error {
	if m.ensureReadyFunc != nil {
		return m.ensureReadyFunc(ctx)
	}
	return nil
}

// mockClient is a mock implementation of Client for testing
type mockClient struct {
	listToolsFunc func(ctx context.Context) ([]*mcp.Tool, error)
	callToolFunc  func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
	closeFunc     func() error
}

func (m *mockClient) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if m.listToolsFunc != nil {
		return m.listToolsFunc(ctx)
	}
	return []*mcp.Tool{}, nil
}

func (m *mockClient) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if m.callToolFunc != nil {
		return m.callToolFunc(ctx, params)
	}
	return &mcp.CallToolResult{
		IsError: false,
		Content: []mcp.Content{
			&mcp.TextContent{Text: "success"},
		},
	}, nil
}

func (m *mockClient) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestNewExecutor(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.OrlaConfig
		expectedErr bool
		errContains string
	}{
		{
			name: "valid config",
			cfg: &config.OrlaConfig{
				Model: "ollama:llama3",
			},
			expectedErr: false,
		},
		{
			name: "invalid model",
			cfg: &config.OrlaConfig{
				Model: "invalid:model",
			},
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, err := NewExecutor(tt.cfg)
			if tt.expectedErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, executor)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, executor)
				assert.Equal(t, tt.cfg, executor.cfg)
				assert.NotNil(t, executor.provider)
			}
		})
	}
}

func TestLoop_NewLoop(t *testing.T) {
	cfg := &config.OrlaConfig{}
	client := &mockClient{}
	provider := &mockProvider{name: "test"}

	loop := NewLoop(client, provider, cfg)
	require.NotNil(t, loop)
	assert.Equal(t, client, loop.client)
	assert.Equal(t, provider, loop.provider)
	assert.Equal(t, cfg, loop.cfg)
}

func TestLoop_Execute_NoToolCalls(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{
				{Name: "test_tool", Description: "A test tool"},
			}, nil
		},
	}

	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			return &model.Response{
				Content:   "Hello, world!",
				ToolCalls: []model.ToolCallWithID{},
			}, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	response, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "Hello, world!", response.Content)
	assert.Empty(t, response.ToolCalls)
}

func TestLoop_Execute_WithToolCalls(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	toolCallID := "call_123"
	toolName := "test_tool"

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{
				{Name: toolName, Description: "A test tool"},
			}, nil
		},
		callToolFunc: func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
			assert.Equal(t, toolName, params.Name)
			return &mcp.CallToolResult{
				IsError: false,
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Tool executed successfully"},
				},
			}, nil
		},
	}

	callCount := 0
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				// First call: model requests tool call
				return &model.Response{
					Content: "",
					ToolCalls: []model.ToolCallWithID{
						{
							ID: toolCallID,
							McpCallToolParams: mcp.CallToolParams{
								Name:      toolName,
								Arguments: map[string]any{"arg1": "value1"},
							},
						},
					},
				}, nil, nil
			}
			// Second call: model returns final response after tool execution
			return &model.Response{
				Content:   "Final response after tool execution",
				ToolCalls: []model.ToolCallWithID{},
			}, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	response, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "Final response after tool execution", response.Content)
	assert.Empty(t, response.ToolCalls)
	assert.Equal(t, 2, callCount) // Should have called provider twice
}

func TestLoop_Execute_Streaming(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    true,
	}

	chunks := []string{"Hello", " ", "world", "!"}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	streamCh := make(chan model.StreamEvent, len(chunks))
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			if stream {
				// Send chunks
				go func() {
					for _, chunk := range chunks {
						streamCh <- &model.ContentEvent{Content: chunk}
					}
					close(streamCh)
				}()
				return &model.Response{
					Content:   "Hello world!",
					ToolCalls: []model.ToolCallWithID{},
				}, streamCh, nil
			}
			return &model.Response{Content: "test"}, nil, nil
		},
	}

	var receivedChunks []string
	streamHandler := func(event model.StreamEvent) error {
		if contentEvent, ok := event.(*model.ContentEvent); ok {
			receivedChunks = append(receivedChunks, contentEvent.Content)
		}
		return nil
	}

	loop := NewLoop(client, provider, cfg)
	response, err := loop.Execute(ctx, "test prompt", nil, true, streamHandler)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, chunks, receivedChunks)
}

func TestLoop_Execute_StreamingError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    true,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	streamCh := make(chan model.StreamEvent, 1)
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			go func() {
				streamCh <- &model.ContentEvent{Content: "chunk"}
				close(streamCh)
			}()
			return &model.Response{
				Content:   "test",
				ToolCalls: []model.ToolCallWithID{},
			}, streamCh, nil
		},
	}

	streamHandler := func(event model.StreamEvent) error {
		return errors.New("stream handler error")
	}

	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test prompt", nil, true, streamHandler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream handler error")
}

func TestLoop_Execute_MaxIterations(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 2, // Low limit to trigger max iterations
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{{Name: "test_tool"}}, nil
		},
		callToolFunc: func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: false,
				Content: []mcp.Content{&mcp.TextContent{Text: "success"}},
			}, nil
		},
	}

	callCount := 0
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			callCount++
			// Always return tool calls to trigger max iterations
			return &model.Response{
				Content: "",
				ToolCalls: []model.ToolCallWithID{
					{
						ID: "call_1",
						McpCallToolParams: mcp.CallToolParams{
							Name: "test_tool",
						},
					},
				},
			}, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum tool call iterations")
}

func TestLoop_Execute_ListToolsError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return nil, errors.New("failed to list tools")
		},
	}

	provider := &mockProvider{}
	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list tools")
}

func TestLoop_Execute_ChatError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			return nil, nil, errors.New("chat error")
		},
	}

	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model chat failed")
}

func TestLoop_Execute_NilResponse(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			return nil, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "received nil response")
}

func TestLoop_Execute_StreamingWithoutHandler(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    true,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	provider := &mockProvider{}
	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test prompt", nil, true, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream handler is required")
}

func TestLoop_Execute_WithExistingMessages(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	var receivedMessages []model.Message
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			receivedMessages = messages
			return &model.Response{
				Content:   "response",
				ToolCalls: []model.ToolCallWithID{},
			}, nil, nil
		},
	}

	existingMessages := []model.Message{
		{Role: model.MessageRoleUser, Content: "previous message"},
	}

	loop := NewLoop(client, provider, cfg)
	response, err := loop.Execute(ctx, "new prompt", existingMessages, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Len(t, receivedMessages, 2)
	assert.Equal(t, "previous message", receivedMessages[0].Content)
	assert.Equal(t, "new prompt", receivedMessages[1].Content)
}

func TestLoop_executeToolCalls(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{}

	callCount := 0
	client := &mockClient{
		callToolFunc: func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
			callCount++
			return &mcp.CallToolResult{
				IsError: false,
				Content: []mcp.Content{
					&mcp.TextContent{Text: "success"},
				},
			}, nil
		},
	}

	provider := &mockProvider{}
	loop := NewLoop(client, provider, cfg)

	toolCalls := []model.ToolCallWithID{
		{
			ID: "call_1",
			McpCallToolParams: mcp.CallToolParams{
				Name:      "tool1",
				Arguments: map[string]any{"arg": "value"},
			},
		},
		{
			ID: "call_2",
			McpCallToolParams: mcp.CallToolParams{
				Name:      "tool2",
				Arguments: map[string]any{"arg": "value2"},
			},
		},
	}

	results := loop.executeToolCalls(ctx, toolCalls)
	require.Len(t, results, 2)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "call_1", results[0].ID)
	assert.Equal(t, "call_2", results[1].ID)
	assert.False(t, results[0].McpCallToolResult.IsError)
	assert.False(t, results[1].McpCallToolResult.IsError)
}

func TestLoop_executeToolCalls_WithError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{}

	client := &mockClient{
		callToolFunc: func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
			return nil, errors.New("tool execution failed")
		},
	}

	provider := &mockProvider{}
	loop := NewLoop(client, provider, cfg)

	toolCalls := []model.ToolCallWithID{
		{
			ID: "call_1",
			McpCallToolParams: mcp.CallToolParams{
				Name: "tool1",
			},
		},
	}

	results := loop.executeToolCalls(ctx, toolCalls)
	require.Len(t, results, 1)
	assert.Equal(t, "call_1", results[0].ID)
	assert.True(t, results[0].McpCallToolResult.IsError)
	require.NotEmpty(t, results[0].McpCallToolResult.Content)
	textContent, ok := results[0].McpCallToolResult.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent")
	assert.Contains(t, textContent.Text, "Tool call failed")
}

func TestFormatToolResult(t *testing.T) {
	tests := []struct {
		name     string
		result   model.ToolResultWithID
		expected string
	}{
		{
			name: "single text content",
			result: model.ToolResultWithID{
				ID: "call_1",
				McpCallToolResult: mcp.CallToolResult{
					IsError: false,
					Content: []mcp.Content{
						&mcp.TextContent{Text: "result text"},
					},
				},
			},
			expected: "result text",
		},
		{
			name: "multiple text contents",
			result: model.ToolResultWithID{
				ID: "call_1",
				McpCallToolResult: mcp.CallToolResult{
					IsError: false,
					Content: []mcp.Content{
						&mcp.TextContent{Text: "line 1"},
						&mcp.TextContent{Text: "line 2"},
					},
				},
			},
			expected: "line 1\nline 2",
		},
		{
			name: "error result",
			result: model.ToolResultWithID{
				ID: "call_1",
				McpCallToolResult: mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{},
				},
			},
			expected: "Tool execution failed",
		},
		{
			name: "empty content",
			result: model.ToolResultWithID{
				ID: "call_1",
				McpCallToolResult: mcp.CallToolResult{
					IsError: false,
					Content: []mcp.Content{},
				},
			},
			expected: "Tool executed successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolResult(tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoop_Execute_ToolResultWithoutMatchingCall(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{{Name: "test_tool"}}, nil
		},
		callToolFunc: func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: false,
				Content: []mcp.Content{&mcp.TextContent{Text: "success"}},
			}, nil
		},
	}

	callCount := 0
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			callCount++
			if callCount == 1 {
				// Return tool call with ID that won't match
				return &model.Response{
					Content: "",
					ToolCalls: []model.ToolCallWithID{
						{
							ID: "call_1",
							McpCallToolParams: mcp.CallToolParams{
								Name: "test_tool",
							},
						},
					},
				}, nil, nil
			}
			return &model.Response{
				Content:   "final",
				ToolCalls: []model.ToolCallWithID{},
			}, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	// This should still work, but the tool result without matching ID will be skipped
	response, err := loop.Execute(ctx, "test", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestLoop_Execute_StreamChannelNil(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    true,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{}, nil
		},
	}

	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			// Return nil stream channel when streaming is enabled
			return &model.Response{
				Content:   "test",
				ToolCalls: []model.ToolCallWithID{},
			}, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	_, err := loop.Execute(ctx, "test", nil, true, func(event model.StreamEvent) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream channel is nil")
}

func TestLoop_Execute_WithContentAndToolCalls(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{
		MaxToolCalls: 10,
		Streaming:    false,
	}

	client := &mockClient{
		listToolsFunc: func(ctx context.Context) ([]*mcp.Tool, error) {
			return []*mcp.Tool{{Name: "test_tool"}}, nil
		},
		callToolFunc: func(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				IsError: false,
				Content: []mcp.Content{&mcp.TextContent{Text: "success"}},
			}, nil
		},
	}

	callCount := 0
	var receivedMessages []model.Message
	provider := &mockProvider{
		chatFunc: func(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool) (*model.Response, <-chan model.StreamEvent, error) {
			callCount++
			receivedMessages = messages
			if callCount == 1 {
				// First call: return both content and tool calls
				return &model.Response{
					Content: "Let me check that for you",
					ToolCalls: []model.ToolCallWithID{
						{
							ID: "call_1",
							McpCallToolParams: mcp.CallToolParams{
								Name: "test_tool",
							},
						},
					},
				}, nil, nil
			}
			// Second call: final response
			return &model.Response{
				Content:   "Here's the result",
				ToolCalls: []model.ToolCallWithID{},
			}, nil, nil
		},
	}

	loop := NewLoop(client, provider, cfg)
	response, err := loop.Execute(ctx, "test", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "Here's the result", response.Content)

	// Verify that the assistant message with content was added to conversation
	assert.Len(t, receivedMessages, 3) // user prompt, assistant content, tool result
	assert.Equal(t, model.MessageRoleAssistant, receivedMessages[1].Role)
	assert.Equal(t, "Let me check that for you", receivedMessages[1].Content)
}

// Client tests
// Note: NewClient requires spawning a subprocess, so it's better tested as an integration test.
// These tests focus on the other Client methods.

func TestClient_ListTools(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		client        *Client
		expectedErr   bool
		errContains   string
		expectedTools int
	}{
		{
			name:        "nil session",
			client:      &Client{McpSession: nil},
			expectedErr: true,
			errContains: "MCP session is not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, err := tt.client.ListTools(ctx)
			if tt.expectedErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, tools)
			} else {
				require.NoError(t, err)
				assert.Len(t, tools, tt.expectedTools)
			}
		})
	}
}

func TestClient_CallTool(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		client      *Client
		params      *mcp.CallToolParams
		expectedErr bool
		errContains string
	}{
		{
			name:   "nil session",
			client: &Client{McpSession: nil},
			params: &mcp.CallToolParams{
				Name:      "test_tool",
				Arguments: map[string]any{},
			},
			expectedErr: true,
			errContains: "MCP session is not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.client.CallTool(ctx, tt.params)
			if tt.expectedErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestClient_Close(t *testing.T) {
	tests := []struct {
		name        string
		client      *Client
		expectedErr bool
	}{
		{
			name:        "nil session and cmd",
			client:      &Client{McpSession: nil, Cmd: nil},
			expectedErr: false,
		},
		{
			name:        "nil session, nil cmd process",
			client:      &Client{McpSession: nil, Cmd: &exec.Cmd{}},
			expectedErr: false,
		},
		{
			name:        "nil session, cmd with nil process",
			client:      &Client{McpSession: nil, Cmd: &exec.Cmd{Process: nil}},
			expectedErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.client.Close()
			if tt.expectedErr {
				require.Error(t, err)
			} else {
				// Close should not error for nil sessions/cmds
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetOrlaBin(t *testing.T) {
	bin, err := getOrlaBin()
	require.NoError(t, err)
	assert.NotEmpty(t, bin)
	// Should return a valid path
	assert.True(t, len(bin) > 0)
}
