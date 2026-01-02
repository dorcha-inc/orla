package agent

import (
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	orlaTesting "github.com/dorcha-inc/orla/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStreamHandler_ContentEvent(t *testing.T) {
	event := &model.ContentEvent{Content: "Hello, world!"}

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	handler := createStreamHandler(nil)
	err = handler(event)
	require.NoError(t, err)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)
	assert.Contains(t, stdout, "Hello, world!", "Stdout should contain the content")
	assert.Empty(t, stderr, "Stderr should be empty for ContentEvent")
}

func TestDefaultStreamHandler_ToolCallEvent_WithNameOnly(t *testing.T) {
	testCases := []struct {
		name           string
		cfg            *config.OrlaConfig
		expectedStderr string
		expectedStdout string
		emptyStdout    bool
		emptyStderr    bool
	}{
		{name: "ShowToolCalls", cfg: &config.OrlaConfig{ShowToolCalls: false}, expectedStderr: "", expectedStdout: "", emptyStdout: true, emptyStderr: true},
		{name: "ShowToolCalls", cfg: &config.OrlaConfig{ShowToolCalls: true}, expectedStderr: "tool call received: test_tool", expectedStdout: "", emptyStdout: true, emptyStderr: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := testCase.cfg

			event := &model.ToolCallEvent{
				Name:      "test_tool",
				Arguments: nil,
			}
			capturedOutput, err := orlaTesting.NewCapturedOutput()
			require.NoError(t, err)

			err = createStreamHandler(cfg)(event)
			require.NoError(t, err)

			stdout, stderr, err := capturedOutput.Stop()
			require.NoError(t, err)

			if testCase.emptyStdout {
				assert.Empty(t, stdout, "Stdout should be empty for ToolCallEvent")
			} else {
				assert.Contains(t, stdout, testCase.expectedStdout, "Stdout should contain the tool call")
			}
			if testCase.emptyStderr {
				assert.Empty(t, stderr, "Stderr should be empty for ToolCallEvent")
			} else {
				assert.Contains(t, stderr, testCase.expectedStderr, "Stderr should contain the tool call")
			}
		})
	}
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

	cfg := &config.OrlaConfig{ShowToolCalls: true}

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	err = createStreamHandler(cfg)(event)
	require.NoError(t, err)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	assert.Empty(t, stdout, "Stdout should be empty for ToolCallEvent")
	assert.Contains(t, stderr, "tool call received: test_tool", "Stderr should contain the tool call")
	assert.Contains(t, stderr, "arg1", "Stderr should contain the arguments")
	assert.Contains(t, stderr, "arg2", "Stderr should contain the arguments")

	cfg.ShowToolCalls = false
	capturedOutput2, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)
	err = createStreamHandler(cfg)(event)
	require.NoError(t, err)

	stdout2, stderr2, err := capturedOutput2.Stop()
	require.NoError(t, err)
	assert.Empty(t, stderr2, "Stderr should be empty for ToolCallEvent when ShowToolCalls is false")
	assert.Empty(t, stdout2, "Stdout should be empty for ToolCallEvent")
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

	cfg := &config.OrlaConfig{ShowToolCalls: true}

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	err = createStreamHandler(cfg)(event)
	require.NoError(t, err)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)
	assert.Empty(t, stdout, "Stdout should be empty for ToolCallEvent")
	assert.Contains(t, stderr, "tool call received: test_tool", "Stderr should contain the tool call")
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

func TestCreateStreamHandler_Thinking(t *testing.T) {
	cfg := &config.OrlaConfig{ShowThinking: true}

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	handler := createStreamHandler(cfg)

	// Test thinking event with thinking enabled
	event := &model.ThinkingEvent{Content: "thinking content"}

	err = handler(event)
	require.NoError(t, err)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)
	assert.Contains(t, stderr, "having a think:", "Stderr should contain the having a think message")
	assert.Contains(t, stderr, "thinking content", "Stderr should contain the thinking content")
	assert.Empty(t, stdout, "Stdout should be empty for ThinkingEvent")

	cfg.ShowThinking = false
	capturedOutput2, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)
	handler = createStreamHandler(cfg)
	err = handler(event)
	require.NoError(t, err)

	stdout2, _, err := capturedOutput2.Stop()
	require.NoError(t, err)
	// When ShowThinking is false, Progress writes to stderr but we don't capture it in this test
	// The thinking content itself should not appear
	assert.Empty(t, stdout2, "Stdout should be empty for ThinkingEvent when ShowThinking is false")
}

func TestCreateStreamHandler_ThinkingToContentTransition(t *testing.T) {
	cfg := &config.OrlaConfig{ShowThinking: true}

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	handler := createStreamHandler(cfg)

	thinkingEvent := &model.ThinkingEvent{Content: "thinking content"}
	err = handler(thinkingEvent)
	require.NoError(t, err)

	contentEvent := &model.ContentEvent{Content: "content content"}
	err = handler(contentEvent)
	require.NoError(t, err)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// tui.Info() and tui.ThinkingMessage() write to stderr, fmt.Print() writes to stdout
	assert.Contains(t, stderr, "having a think:", "Stderr should contain the having a think message")
	assert.Contains(t, stderr, "thinking content", "Stderr should contain the thinking content")
	assert.Contains(t, stderr, "completed the think", "Stderr should contain the completed the think message")
	assert.Contains(t, stdout, "content content", "Stdout should contain the content")
}

func TestCreateStreamHandler_ThinkingToToolCallTransition(t *testing.T) {
	cfg := &config.OrlaConfig{ShowThinking: true, ShowToolCalls: true}
	handler := createStreamHandler(cfg)

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	// Start with thinking
	thinkingEvent := &model.ThinkingEvent{Content: "thinking"}
	err = handler(thinkingEvent)
	require.NoError(t, err)

	// Then tool call (should complete thinking)
	toolCallEvent := &model.ToolCallEvent{
		Name:      "test_tool",
		Arguments: map[string]any{},
	}
	err = handler(toolCallEvent)
	require.NoError(t, err)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// Thinking content and tool calls both go to stderr (metadata)
	assert.Contains(t, stderr, "having a think:", "Stderr should contain the having a think message")
	assert.Contains(t, stderr, "thinking", "Stderr should contain the thinking content")
	assert.Contains(t, stderr, "completed the think", "Stderr should contain the completed the think message")
	assert.Contains(t, stderr, "tool call received: test_tool", "Stderr should contain the tool call")
	assert.Empty(t, stdout, "Stdout should be empty")
}

func TestCreateStreamHandler_MultipleThinkingEvents(t *testing.T) {
	cfg := &config.OrlaConfig{ShowThinking: true}
	handler := createStreamHandler(cfg)

	// First thinking event
	event1 := &model.ThinkingEvent{Content: "thinking 1"}
	err := handler(event1)
	assert.NoError(t, err)

	// Second thinking event (should continue, not restart)
	event2 := &model.ThinkingEvent{Content: "thinking 2"}
	err = handler(event2)
	assert.NoError(t, err)
}

func TestCreateStreamHandler_DuplicateToolNames(t *testing.T) {
	cfg := &config.OrlaConfig{ShowToolCalls: false}
	handler := createStreamHandler(cfg)

	// Add same tool twice (should not duplicate in list)
	event1 := &model.ToolCallEvent{
		Name:      "test_tool",
		Arguments: map[string]any{},
	}
	err := handler(event1)
	assert.NoError(t, err)

	event2 := &model.ToolCallEvent{
		Name:      "test_tool",
		Arguments: map[string]any{},
	}
	err = handler(event2)
	assert.NoError(t, err)
}

func TestExecuteAgentPrompt_EmptyPrompt(t *testing.T) {
	// Test that ExecuteAgentPrompt handles empty prompt
	err := ExecuteAgentPrompt("", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestExecuteAgentPrompt_ModelOverride(t *testing.T) {
	// Test that model override is applied
	// We can verify the model override is passed through by checking error messages
	err := ExecuteAgentPrompt("test prompt", "invalid-model-override")
	// Should fail because the model override format is invalid
	require.Error(t, err)
	// The error should indicate the model override was attempted and failed validation
	assert.Contains(t, err.Error(), "invalid-model-override", "Error should mention the model override that was attempted")
}
