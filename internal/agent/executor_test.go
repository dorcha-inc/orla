package agent

import (
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStreamHandler_ContentEvent(t *testing.T) {
	event := &model.ContentEvent{Content: "Hello, world!"}
	err := createStreamHandler(nil)(event)
	assert.NoError(t, err)
}

func TestDefaultStreamHandler_ToolCallEvent_WithNameOnly(t *testing.T) {
	event := &model.ToolCallEvent{
		Name:      "test_tool",
		Arguments: nil,
	}
	err := createStreamHandler(nil)(event)
	assert.NoError(t, err)
}

func TestDefaultStreamHandler_ToolCallEvent_WithEmptyName(t *testing.T) {
	event := &model.ToolCallEvent{
		Name:      "",
		Arguments: map[string]any{},
	}
	err := createStreamHandler(nil)(event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool call name is empty")
}

func TestDefaultStreamHandler_ToolCallEvent_WithArguments(t *testing.T) {
	event := &model.ToolCallEvent{
		Name: "test_tool",
		Arguments: map[string]any{
			"arg1": "value1",
			"arg2": 42,
		},
	}
	err := createStreamHandler(nil)(event)
	assert.NoError(t, err)
}

func TestDefaultStreamHandler_ToolCallEvent_WithUnmarshalableArguments(t *testing.T) {
	// Use a channel which cannot be marshaled to JSON
	ch := make(chan int)
	event := &model.ToolCallEvent{
		Name: "test_tool",
		Arguments: map[string]any{
			"channel": ch,
		},
	}
	// Need to enable ShowToolCalls to test the marshaling error path
	cfg := &config.OrlaConfig{ShowToolCalls: true}
	err := createStreamHandler(cfg)(event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal tool call arguments")
}

func TestDefaultStreamHandler_ToolCallEvent_WithEmptyArguments(t *testing.T) {
	event := &model.ToolCallEvent{
		Name:      "test_tool",
		Arguments: map[string]any{},
	}
	err := createStreamHandler(nil)(event)
	assert.NoError(t, err)
}

func TestDefaultStreamHandler_UnknownEventType(t *testing.T) {
	// Create an event that doesn't match any known type
	type UnknownEvent struct {
		model.StreamEvent
		Data string
	}
	event := &UnknownEvent{Data: "unknown"}
	err := createStreamHandler(nil)(event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown stream event type")
}
