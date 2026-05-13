package model

import (
	"encoding/json"
	"testing"

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

func TestToolCall(t *testing.T) {
	toolCall := ToolCall{
		ID:        "call-123",
		Name:      "test_tool",
		Arguments: json.RawMessage(`{"arg1":"value1"}`),
	}

	assert.Equal(t, "call-123", toolCall.ID)
	assert.Equal(t, "test_tool", toolCall.Name)

	var args map[string]any
	require.NoError(t, json.Unmarshal(toolCall.Arguments, &args))
	assert.Equal(t, "value1", args["arg1"])
}

func TestResponse_Empty(t *testing.T) {
	response := &Response{}

	assert.Empty(t, response.Content)
	assert.Empty(t, response.Thinking)
	assert.Empty(t, response.ToolCalls)
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
		ToolCalls: []ToolCall{
			{ID: "call-1", Name: "test_tool"},
		},
	}

	assert.Equal(t, "I'll call a tool", response.Content)
	assert.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "call-1", response.ToolCalls[0].ID)
	assert.Equal(t, "test_tool", response.ToolCalls[0].Name)
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
