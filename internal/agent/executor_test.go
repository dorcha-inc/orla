package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/harvard-cns/orla/internal/config"
	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	orlaTesting "github.com/harvard-cns/orla/internal/testing"
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

func TestExecuteAgentPrompt_EmptyPrompt(t *testing.T) {
	// Test that ExecuteAgentPrompt handles empty prompt
	err := ExecuteAgentPrompt("", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestExecuteAgentPrompt_ModelOverride(t *testing.T) {
	// Test that model override is applied
	// We can verify the model override is passed through by checking error messages
	err := ExecuteAgentPrompt("test prompt", "invalid-model-override", "")
	// Should fail because the model override format is invalid
	require.Error(t, err)
	// The error should indicate the model override was attempted and failed validation
	assert.Contains(t, err.Error(), "invalid-model-override", "Error should mention the model override that was attempted")
}

func TestCreateContextWithSignals(t *testing.T) {
	// Test that createContextWithSignals creates a context and cancel function
	ctx, cancel := createContextWithSignals()
	require.NotNil(t, ctx)
	require.NotNil(t, cancel)

	// Context should be active initially
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled initially")
	default:
		// Good, context is active
	}

	// Cancel should work
	cancel()

	// Context should be cancelled after cancel is called
	select {
	case <-ctx.Done():
		// Good, context is cancelled
	default:
		t.Fatal("Context should be cancelled after cancel() is called")
	}
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
				Model:      "openai:llama3",
				LLMBackend: &core.LLMBackend{Endpoint: "http://localhost:11434/v1"},
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

func TestExecutor_Execute_NoToolCalls(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: false}

	provider := model.NewMockProvider().WithContent("Hello, world!").Build()

	executor := &Executor{provider: provider, cfg: cfg}
	response, err := executor.Execute(ctx, "test prompt", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "Hello, world!", response.Content)
	assert.Empty(t, response.ToolCalls)
}

func TestExecutor_Execute_Streaming(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: true}

	chunks := []string{"Hello", " ", "world", "!"}

	streamCh := make(chan model.StreamEvent, len(chunks))
	provider := model.NewMockProvider().WithChatFunc(func(ctx context.Context, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
		if opts.Stream {
			go func() {
				for _, chunk := range chunks {
					streamCh <- &model.ContentEvent{Content: chunk}
				}
				close(streamCh)
			}()
			return &model.Response{
				Content:   "Hello world!",
				ToolCalls: []model.ToolCall{},
			}, streamCh, nil
		}
		return &model.Response{Content: "test"}, nil, nil
	}).Build()

	var receivedChunks []string
	streamHandler := func(event model.StreamEvent) error {
		if contentEvent, ok := event.(*model.ContentEvent); ok {
			receivedChunks = append(receivedChunks, contentEvent.Content)
		}
		return nil
	}

	executor := &Executor{provider: provider, cfg: cfg}
	response, err := executor.Execute(ctx, "test prompt", nil, true, streamHandler)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, chunks, receivedChunks)
}

func TestExecutor_Execute_StreamingError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: true}

	streamCh := make(chan model.StreamEvent, 1)
	provider := model.NewMockProvider().WithChatFunc(func(ctx context.Context, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
		go func() {
			streamCh <- &model.ContentEvent{Content: "chunk"}
			close(streamCh)
		}()
		return &model.Response{
			Content:   "test",
			ToolCalls: []model.ToolCall{},
		}, streamCh, nil
	}).Build()

	streamHandler := func(event model.StreamEvent) error {
		return errors.New("stream handler error")
	}

	executor := &Executor{provider: provider, cfg: cfg}
	_, err := executor.Execute(ctx, "test prompt", nil, true, streamHandler)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream handler error")
}

func TestExecutor_Execute_ChatError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: false}

	provider := model.NewMockProvider().WithChatError(errors.New("chat error")).Build()

	executor := &Executor{provider: provider, cfg: cfg}
	_, err := executor.Execute(ctx, "test prompt", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model chat failed")
}

func TestExecutor_Execute_NilResponse(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: false}

	provider := model.NewMockProvider().WithChatFunc(func(ctx context.Context, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
		return nil, nil, nil
	}).Build()

	executor := &Executor{provider: provider, cfg: cfg}
	_, err := executor.Execute(ctx, "test prompt", nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "received nil response")
}

func TestExecutor_Execute_StreamingWithoutHandler(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: true}

	provider := model.NewMockProvider().Build()
	executor := &Executor{provider: provider, cfg: cfg}
	_, err := executor.Execute(ctx, "test prompt", nil, true, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream handler is required")
}

func TestExecutor_Execute_WithExistingMessages(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: false}

	provider := model.NewMockProvider().WithContent("response").Build()

	existingMessages := []model.Message{
		{Role: model.MessageRoleUser, Content: "previous message"},
	}

	executor := &Executor{provider: provider, cfg: cfg}
	response, err := executor.Execute(ctx, "new prompt", existingMessages, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	receivedMessages := provider.ReceivedMessages()
	require.Len(t, receivedMessages, 1)
	assert.Len(t, receivedMessages[0], 2)
	assert.Equal(t, "previous message", receivedMessages[0][0].Content)
	assert.Equal(t, "new prompt", receivedMessages[0][1].Content)
}

func TestExecutor_Execute_StreamChannelNil(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: true}

	provider := model.NewMockProvider().WithChatFunc(func(ctx context.Context, messages []model.Message, tools []*model.Tool, opts model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
		return &model.Response{
			Content:   "test",
			ToolCalls: []model.ToolCall{},
		}, nil, nil
	}).Build()

	executor := &Executor{provider: provider, cfg: cfg}
	_, err := executor.Execute(ctx, "test", nil, true, func(event model.StreamEvent) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream channel is nil")
}

func TestExecutor_Execute_WithContent(t *testing.T) {
	ctx := context.Background()
	cfg := &config.OrlaConfig{Streaming: false}

	provider := model.NewMockProvider().WithContent("Here's the result").Build()

	executor := &Executor{provider: provider, cfg: cfg}
	response, err := executor.Execute(ctx, "test", nil, false, nil)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "Here's the result", response.Content)
	receivedMessages := provider.ReceivedMessages()
	require.Len(t, receivedMessages, 1)
	assert.Len(t, receivedMessages[0], 1)
	assert.Equal(t, "test", receivedMessages[0][0].Content)
}
