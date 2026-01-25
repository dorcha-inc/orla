package model

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageRole_String(t *testing.T) {
	assert.Equal(t, "user", MessageRoleUser.String())
	assert.Equal(t, "assistant", MessageRoleAssistant.String())
	assert.Equal(t, "system", MessageRoleSystem.String())
	assert.Equal(t, "tool", MessageRoleTool.String())
}

func TestMessage_UserMessage(t *testing.T) {
	msg := Message{
		Role:    MessageRoleUser,
		Content: "Hello",
	}

	assert.Equal(t, MessageRoleUser, msg.Role)
	assert.Equal(t, "Hello", msg.Content)
	assert.Empty(t, msg.ToolName)
	assert.Empty(t, msg.ToolCallID)
}

func TestMessage_ToolMessage(t *testing.T) {
	msg := Message{
		Role:       MessageRoleTool,
		Content:    "tool result",
		ToolName:   "test_tool",
		ToolCallID: "call-123",
	}

	assert.Equal(t, MessageRoleTool, msg.Role)
	assert.Equal(t, "tool result", msg.Content)
	assert.Equal(t, "test_tool", msg.ToolName)
	assert.Equal(t, "call-123", msg.ToolCallID)
}

func TestToolCallWithID(t *testing.T) {
	toolCall := ToolCallWithID{
		ID: "call-123",
		McpCallToolParams: mcp.CallToolParams{
			Name:      "test_tool",
			Arguments: map[string]any{"arg1": "value1"},
		},
	}

	assert.Equal(t, "call-123", toolCall.ID)
	assert.Equal(t, "test_tool", toolCall.McpCallToolParams.Name)
	args, ok := toolCall.McpCallToolParams.Arguments.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value1", args["arg1"])
}

func TestToolResultWithID(t *testing.T) {
	toolResult := ToolResultWithID{
		ID: "call-123",
		McpCallToolResult: mcp.CallToolResult{
			IsError: false,
			Content: []mcp.Content{
				&mcp.TextContent{Text: "result"},
			},
		},
	}

	assert.Equal(t, "call-123", toolResult.ID)
	assert.False(t, toolResult.McpCallToolResult.IsError)
	assert.Len(t, toolResult.McpCallToolResult.Content, 1)
}

func TestResponse_Empty(t *testing.T) {
	response := &Response{}

	assert.Empty(t, response.Content)
	assert.Empty(t, response.Thinking)
	assert.Empty(t, response.ToolCalls)
	assert.Empty(t, response.ToolResults)
}

func TestResponse_WithContent(t *testing.T) {
	response := &Response{
		Content:  "Hello world",
		Thinking: "I should say hello",
	}

	assert.Equal(t, "Hello world", response.Content)
	assert.Equal(t, "I should say hello", response.Thinking)
}

func TestResponse_WithToolCalls(t *testing.T) {
	response := &Response{
		Content: "I'll call a tool",
		ToolCalls: []ToolCallWithID{
			{
				ID: "call-1",
				McpCallToolParams: mcp.CallToolParams{
					Name: "test_tool",
				},
			},
		},
	}

	assert.Equal(t, "I'll call a tool", response.Content)
	assert.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "call-1", response.ToolCalls[0].ID)
	assert.Equal(t, "test_tool", response.ToolCalls[0].McpCallToolParams.Name)
}

func TestStreamEventType_String(t *testing.T) {
	assert.Equal(t, "content", string(StreamEventTypeContent))
	assert.Equal(t, "toolcall", string(StreamEventTypeToolCall))
	assert.Equal(t, "thinking", string(StreamEventTypeThinking))
}

func TestContentEvent_Type(t *testing.T) {
	event := &ContentEvent{
		Content: "chunk",
	}

	assert.Equal(t, StreamEventTypeContent, event.Type())
	assert.Equal(t, "chunk", event.Content)
}

func TestToolCallEvent_Type(t *testing.T) {
	event := &ToolCallEvent{
		Name:      "test_tool",
		Arguments: map[string]any{"arg": "value"},
	}

	assert.Equal(t, StreamEventTypeToolCall, event.Type())
	assert.Equal(t, "test_tool", event.Name)
	assert.Equal(t, "value", event.Arguments["arg"])
}

func TestThinkingEvent_Type(t *testing.T) {
	event := &ThinkingEvent{
		Content: "thinking chunk",
	}

	assert.Equal(t, StreamEventTypeThinking, event.Type())
	assert.Equal(t, "thinking chunk", event.Content)
}
