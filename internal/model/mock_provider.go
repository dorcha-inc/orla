// Package model provides the Provider interface and implementations.
// This file contains MockProvider for testing.
package model

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// MockProvider is a mock implementation of Provider for testing.
// Supports both builder-based configuration and function-based customization.
type MockProvider struct {
	mu sync.RWMutex

	name            string
	chatFunc        func(ctx context.Context, messages []Message, tools []*Tool, opts InferenceOptions) (*Response, <-chan StreamEvent, error)
	ensureReadyFunc func(ctx context.Context) error

	// Builder-configured response (used when chatFunc is nil)
	chatResponse     *Response
	chatStreamCh     <-chan StreamEvent
	chatError        error
	ensureReadyError error

	// Inspection: recorded from Chat calls
	receivedMessages    [][]Message
	lastInferenceOptions *InferenceOptions
	callCount           int
}

// MockProviderBuilder builds a MockProvider with a fluent API.
type MockProviderBuilder struct {
	p *MockProvider
}

// NewMockProvider returns a new MockProviderBuilder.
func NewMockProvider() *MockProviderBuilder {
	return &MockProviderBuilder{p: &MockProvider{name: "mock"}}
}

// WithName sets the provider name.
func (b *MockProviderBuilder) WithName(name string) *MockProviderBuilder {
	b.p.name = name
	return b
}

// WithContent sets the response content (non-streaming).
func (b *MockProviderBuilder) WithContent(content string) *MockProviderBuilder {
	b.p.chatResponse = &Response{Content: content, ToolCalls: []ToolCall{}}
	return b
}

// WithToolCall adds a tool call to the response. argsJSON is the JSON-encoded arguments.
func (b *MockProviderBuilder) WithToolCall(name, argsJSON string) *MockProviderBuilder {
	if b.p.chatResponse == nil {
		b.p.chatResponse = &Response{}
	}
	var args json.RawMessage
	if argsJSON != "" {
		args = json.RawMessage(argsJSON)
	}
	b.p.chatResponse.ToolCalls = append(b.p.chatResponse.ToolCalls, ToolCall{
		ID:        "call_" + name,
		Name:      name,
		Arguments: args,
	})
	return b
}

// WithStreamChunks configures streaming response with the given chunks.
func (b *MockProviderBuilder) WithStreamChunks(chunks []string) *MockProviderBuilder {
	ch := make(chan StreamEvent, len(chunks))
	go func() {
		for _, c := range chunks {
			ch <- &ContentEvent{Content: c}
		}
		close(ch)
	}()
	b.p.chatStreamCh = ch
	if b.p.chatResponse == nil {
		b.p.chatResponse = &Response{}
	}
	b.p.chatResponse.Content = ""
	for _, c := range chunks {
		b.p.chatResponse.Content += c
	}
	return b
}

// WithChatError configures the provider to return an error from Chat.
func (b *MockProviderBuilder) WithChatError(err error) *MockProviderBuilder {
	b.p.chatError = err
	return b
}

// WithEnsureReadyError configures the provider to return an error from EnsureReady.
func (b *MockProviderBuilder) WithEnsureReadyError(err error) *MockProviderBuilder {
	b.p.ensureReadyError = err
	return b
}

// WithChatFunc sets a custom Chat implementation (overrides builder-configured response).
func (b *MockProviderBuilder) WithChatFunc(fn func(ctx context.Context, messages []Message, tools []*Tool, opts InferenceOptions) (*Response, <-chan StreamEvent, error)) *MockProviderBuilder {
	b.p.chatFunc = fn
	return b
}

// Build returns the configured MockProvider.
func (b *MockProviderBuilder) Build() *MockProvider {
	return b.p
}

// Name returns the provider name.
func (m *MockProvider) Name() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.name
}

// Chat sends a chat request and returns the configured or custom response.
func (m *MockProvider) Chat(ctx context.Context, messages []Message, tools []*Tool, opts InferenceOptions) (*Response, <-chan StreamEvent, error) {
	if m == nil {
		return nil, nil, errors.New("nil mock provider")
	}
	m.mu.Lock()
	m.callCount++
	m.receivedMessages = append(m.receivedMessages, append([]Message(nil), messages...))
	optsCopy := opts
	m.lastInferenceOptions = &optsCopy
	m.mu.Unlock()

	if m.chatFunc != nil {
		return m.chatFunc(ctx, messages, tools, opts)
	}
	if m.chatError != nil {
		return nil, nil, m.chatError
	}
	if m.chatResponse == nil {
		return &Response{Content: "test response", ToolCalls: []ToolCall{}}, nil, nil
	}
	return m.chatResponse, m.chatStreamCh, nil
}

// EnsureReady returns nil or the configured error.
func (m *MockProvider) EnsureReady(ctx context.Context) error {
	if m == nil {
		return errors.New("nil mock provider")
	}
	if m.ensureReadyFunc != nil {
		return m.ensureReadyFunc(ctx)
	}
	return m.ensureReadyError
}

// ReceivedMessages returns a copy of all message slices passed to Chat.
func (m *MockProvider) ReceivedMessages() [][]Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([][]Message, len(m.receivedMessages))
	for i, msgs := range m.receivedMessages {
		out[i] = append([]Message(nil), msgs...)
	}
	return out
}

// LastInferenceOptions returns the options from the most recent Chat call.
func (m *MockProvider) LastInferenceOptions() *InferenceOptions {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastInferenceOptions == nil {
		return nil
	}
	opts := *m.lastInferenceOptions
	return &opts
}

// CallCount returns the number of times Chat has been called.
func (m *MockProvider) CallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.callCount
}

// Ensure MockProvider implements Provider.
var _ Provider = (*MockProvider)(nil)
