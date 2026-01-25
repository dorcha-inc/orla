package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoop(t *testing.T) {
	mockClient := &mockMCPClient{}
	mockProvider := &mockProvider{name: "test"}
	cfg := &config.OrlaConfig{MaxToolCalls: 5}

	loop := NewLoop(mockClient, mockProvider, cfg)

	assert.NotNil(t, loop)
	assert.Equal(t, mockClient, loop.client)
	assert.Equal(t, mockProvider, loop.provider)
	assert.Equal(t, cfg, loop.cfg)
}

func TestLoop_Execute_Success_NoToolCalls(t *testing.T) {
	mockClient := &mockMCPClient{
		tools: []*mcp.Tool{
			{Name: "test_tool", Description: "A test tool"},
		},
	}
	mockProvider := &mockProvider{
		name: "test",
		chatResponse: &model.Response{
			Content:   "Hello world",
			ToolCalls: []model.ToolCallWithID{},
		},
	}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	response, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "Hello world", response.Content)
	assert.Empty(t, response.ToolCalls)
}
func TestLoop_Execute_StreamHandlerRequired(t *testing.T) {
	mockClient := &mockMCPClient{
		tools: []*mcp.Tool{},
	}
	mockProvider := &mockProvider{
		name: "test",
		chatResponse: &model.Response{
			Content: "response",
		},
	}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	// Stream is true but handler is nil
	_, err := loop.Execute(ctx, "test prompt", nil, true, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream handler is required")
}

func TestLoop_Execute_MaxIterationsReached(t *testing.T) {
	mockClient := &mockMCPClient{
		tools: []*mcp.Tool{
			{Name: "test_tool"},
		},
		callResult: &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "result"}},
		},
	}
	// Always return tool calls to trigger max iterations
	mockProvider := &mockProvider{
		name: "test",
		chatResponse: &model.Response{
			Content: "calling tool",
			ToolCalls: []model.ToolCallWithID{
				{
					ID: "call-1",
					McpCallToolParams: mcp.CallToolParams{
						Name: "test_tool",
					},
				},
			},
		},
	}
	cfg := &config.OrlaConfig{MaxToolCalls: 2} // Low limit

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	_, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maximum tool call iterations")
}

func TestLoop_Execute_DefaultMaxIterations(t *testing.T) {
	mockClient := &mockMCPClient{
		tools: []*mcp.Tool{},
	}
	mockProvider := &mockProvider{
		name: "test",
		chatResponse: &model.Response{
			Content:   "response",
			ToolCalls: []model.ToolCallWithID{},
		},
	}
	cfg := &config.OrlaConfig{MaxToolCalls: 0} // Should default to 10

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	response, err := loop.Execute(ctx, "test prompt", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestLoop_Execute_WithMessages(t *testing.T) {
	mockClient := &mockMCPClient{
		tools: []*mcp.Tool{},
	}
	mockProvider := &mockProvider{
		name: "test",
		chatResponse: &model.Response{
			Content:   "response",
			ToolCalls: []model.ToolCallWithID{},
		},
	}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	messages := []model.Message{
		{Role: model.MessageRoleUser, Content: "previous message"},
	}

	response, err := loop.Execute(ctx, "new prompt", messages, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestLoop_Execute_EmptyPrompt(t *testing.T) {
	mockClient := &mockMCPClient{
		tools: []*mcp.Tool{},
	}
	mockProvider := &mockProvider{
		name: "test",
		chatResponse: &model.Response{
			Content:   "response",
			ToolCalls: []model.ToolCallWithID{},
		},
	}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	// Empty prompt should still work (continuing conversation)
	response, err := loop.Execute(ctx, "", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestLoop_executeToolCalls_Success(t *testing.T) {
	mockClient := &mockMCPClient{
		callResult: &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "success"},
			},
		},
	}
	mockProvider := &mockProvider{name: "test"}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	toolCalls := []model.ToolCallWithID{
		{
			ID: "call-1",
			McpCallToolParams: mcp.CallToolParams{
				Name:      "test_tool",
				Arguments: map[string]any{"arg": "value"},
			},
		},
	}

	results := loop.executeToolCalls(ctx, toolCalls)
	require.Len(t, results, 1)
	assert.Equal(t, "call-1", results[0].ID)
	assert.False(t, results[0].McpCallToolResult.IsError)
}

func TestLoop_executeToolCalls_Error(t *testing.T) {
	mockClient := &mockMCPClient{
		callError: errors.New("tool call failed"),
	}
	mockProvider := &mockProvider{name: "test"}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	toolCalls := []model.ToolCallWithID{
		{
			ID: "call-1",
			McpCallToolParams: mcp.CallToolParams{
				Name: "test_tool",
			},
		},
	}

	results := loop.executeToolCalls(ctx, toolCalls)
	require.Len(t, results, 1)
	assert.Equal(t, "call-1", results[0].ID)
	assert.True(t, results[0].McpCallToolResult.IsError)
	require.Greater(t, len(results[0].McpCallToolResult.Content), 0, "Error result should have content")
	textContent, ok := results[0].McpCallToolResult.Content[0].(*mcp.TextContent)
	require.True(t, ok, "First content should be TextContent")
	assert.Contains(t, textContent.Text, "Tool call failed")
}

func TestLoop_executeToolCalls_MultipleTools(t *testing.T) {
	mockClient := &mockMCPClient{
		callResult: &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "result"},
			},
		},
	}

	mockProvider := &mockProvider{name: "test"}
	cfg := &config.OrlaConfig{MaxToolCalls: 10}

	loop := NewLoop(mockClient, mockProvider, cfg)
	ctx := context.Background()

	toolCalls := []model.ToolCallWithID{
		{
			ID: "call-1",
			McpCallToolParams: mcp.CallToolParams{
				Name: "tool1",
			},
		},
		{
			ID: "call-2",
			McpCallToolParams: mcp.CallToolParams{
				Name: "tool2",
			},
		},
	}

	results := loop.executeToolCalls(ctx, toolCalls)
	assert.Len(t, results, 2)
	assert.Equal(t, "call-1", results[0].ID)
	assert.Equal(t, "call-2", results[1].ID)
}

func TestFormatToolResult_TextContent(t *testing.T) {
	result := model.ToolResultWithID{
		ID: "call-1",
		McpCallToolResult: mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "result text"},
			},
		},
	}

	formatted := formatToolResult(result)
	assert.Equal(t, "result text", formatted)
}

func TestFormatToolResult_MultipleTextContent(t *testing.T) {
	result := model.ToolResultWithID{
		ID: "call-1",
		McpCallToolResult: mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "first"},
				&mcp.TextContent{Text: "second"},
			},
		},
	}

	formatted := formatToolResult(result)
	assert.Contains(t, formatted, "first")
	assert.Contains(t, formatted, "second")
}

func TestFormatToolResult_EmptyContent_Error(t *testing.T) {
	result := model.ToolResultWithID{
		ID: "call-1",
		McpCallToolResult: mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{},
		},
	}

	formatted := formatToolResult(result)
	assert.Equal(t, "Tool execution failed", formatted)
}

func TestFormatToolResult_EmptyContent_Success(t *testing.T) {
	result := model.ToolResultWithID{
		ID: "call-1",
		McpCallToolResult: mcp.CallToolResult{
			IsError: false,
			Content: []mcp.Content{},
		},
	}

	formatted := formatToolResult(result)
	assert.Equal(t, "Tool executed successfully", formatted)
}

func TestFormatToolResult_ImageContent(t *testing.T) {
	result := model.ToolResultWithID{
		ID: "call-1",
		McpCallToolResult: mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.ImageContent{Data: []byte{1, 2, 3}},
			},
		},
	}

	// Image content should be skipped (only text content is formatted)
	formatted := formatToolResult(result)
	// Should fall back to default message since no text content
	assert.Equal(t, "Tool executed successfully", formatted)
}
