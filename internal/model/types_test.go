package model

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestMessageRole_String(t *testing.T) {
	tests := []struct {
		name     string
		role     MessageRole
		expected string
	}{
		{"user", MessageRoleUser, "user"},
		{"assistant", MessageRoleAssistant, "assistant"},
		{"system", MessageRoleSystem, "system"},
		{"tool", MessageRoleTool, "tool"},
		{"custom", MessageRole("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.role.String())
		})
	}
}

func TestStreamEventTypes(t *testing.T) {
	t.Run("ContentEvent", func(t *testing.T) {
		e := &ContentEvent{Content: "test"}
		assert.Equal(t, StreamEventTypeContent, e.Type())
	})

	t.Run("ToolCallEvent", func(t *testing.T) {
		e := &ToolCallEvent{Name: "test"}
		assert.Equal(t, StreamEventTypeToolCall, e.Type())
	})

	t.Run("ThinkingEvent", func(t *testing.T) {
		e := &ThinkingEvent{Content: "test"}
		assert.Equal(t, StreamEventTypeThinking, e.Type())
	})
}

func TestMessage(t *testing.T) {
	msg := Message{
		Role:    MessageRoleUser,
		Content: "Hello, world!",
	}

	assert.Equal(t, MessageRoleUser, msg.Role)
	assert.Equal(t, "Hello, world!", msg.Content)
}

func TestToolCallWithID(t *testing.T) {
	toolCall := ToolCallWithID{
		ID: "call_123",
		McpCallToolParams: mcp.CallToolParams{
			Name:      "test_tool",
			Arguments: map[string]any{"arg1": "value1"},
		},
	}

	assert.Equal(t, "call_123", toolCall.ID)
	assert.Equal(t, "test_tool", toolCall.McpCallToolParams.Name)
	assert.Equal(t, map[string]any{"arg1": "value1"}, toolCall.McpCallToolParams.Arguments)
}

func TestToolResultWithID(t *testing.T) {
	toolResult := ToolResultWithID{
		ID: "call_123",
		McpCallToolResult: mcp.CallToolResult{
			IsError: false,
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Success"},
			},
		},
	}

	assert.Equal(t, "call_123", toolResult.ID)
	assert.False(t, toolResult.McpCallToolResult.IsError)
	assert.Len(t, toolResult.McpCallToolResult.Content, 1)
}

func TestResponse(t *testing.T) {
	response := Response{
		Content: "Hello!",
		ToolCalls: []ToolCallWithID{
			{
				ID: "call_1",
				McpCallToolParams: mcp.CallToolParams{
					Name:      "test_tool",
					Arguments: map[string]any{"arg": "value"},
				},
			},
		},
		ToolResults: []ToolResultWithID{
			{
				ID: "call_1",
				McpCallToolResult: mcp.CallToolResult{
					IsError: false,
					Content: []mcp.Content{&mcp.TextContent{Text: "result"}},
				},
			},
		},
	}

	assert.Equal(t, "Hello!", response.Content)
	assert.Len(t, response.ToolCalls, 1)
	assert.Len(t, response.ToolResults, 1)
	assert.Equal(t, "call_1", response.ToolCalls[0].ID)
	assert.Equal(t, "call_1", response.ToolResults[0].ID)
}
