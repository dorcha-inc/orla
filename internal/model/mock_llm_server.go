// Package model provides the Provider interface and implementations.
// This file contains MockLLMServer for testing - an HTTP server that speaks the OpenAI-compatible chat API.
package model

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"

	"log/slog"
)

// MockLLMServer is an HTTP server that speaks the OpenAI-compatible chat API.
// Used for integration tests without real API keys or external backends.
type MockLLMServer struct {
	server      *httptest.Server
	mu          sync.RWMutex
	config      mockLLMServerConfig
	lastRequest []byte
}

type mockLLMServerConfig struct {
	content                   string
	toolCalls                 []mockLLMToolCall
	streamChunks              []string
	streamContentAndToolCalls *struct {
		Content   string
		ToolCalls []mockLLMToolCall
	}
	noChoices bool
}

type mockLLMToolCall struct {
	ID   string
	Name string
	Args string
}

// MockLLMServerBuilder builds a MockLLMServer with a fluent API.
type MockLLMServerBuilder struct {
	config mockLLMServerConfig
}

// NewMockLLMServer returns a new MockLLMServerBuilder.
func NewMockLLMServer() *MockLLMServerBuilder {
	return &MockLLMServerBuilder{}
}

// ReturnContent sets the response content for non-streaming.
func (b *MockLLMServerBuilder) ReturnContent(content string) *MockLLMServerBuilder {
	b.config.content = content
	return b
}

// ReturnToolCall adds a tool call to the response. argsJSON is the JSON-encoded arguments.
func (b *MockLLMServerBuilder) ReturnToolCall(name, argsJSON string) *MockLLMServerBuilder {
	return b.ReturnToolCallWithID("call_"+name, name, argsJSON)
}

// ReturnToolCallWithID adds a tool call with a specific ID.
func (b *MockLLMServerBuilder) ReturnToolCallWithID(id, name, argsJSON string) *MockLLMServerBuilder {
	b.config.toolCalls = append(b.config.toolCalls, mockLLMToolCall{
		ID:   id,
		Name: name,
		Args: argsJSON,
	})
	return b
}

// ReturnStreamChunks sets streaming response chunks.
func (b *MockLLMServerBuilder) ReturnStreamChunks(chunks []string) *MockLLMServerBuilder {
	b.config.streamChunks = chunks
	return b
}

// ReturnNoChoices configures the server to return an empty choices array (triggers "no choices" error).
func (b *MockLLMServerBuilder) ReturnNoChoices() *MockLLMServerBuilder {
	b.config.noChoices = true
	return b
}

// ReturnStreamWithToolCalls configures streaming response with content followed by tool calls.
func (b *MockLLMServerBuilder) ReturnStreamWithToolCalls(content string, toolCalls ...mockLLMToolCall) *MockLLMServerBuilder {
	b.config.streamContentAndToolCalls = &struct {
		Content   string
		ToolCalls []mockLLMToolCall
	}{Content: content, ToolCalls: toolCalls}
	return b
}

// Start starts the server and returns it. Call Close when done.
func (b *MockLLMServerBuilder) Start() *MockLLMServer {
	s := &MockLLMServer{config: b.config}
	s.server = httptest.NewServer(http.HandlerFunc(s.handleChat))
	return s
}

// URL returns the server URL.
func (s *MockLLMServer) URL() string {
	if s.server == nil {
		return ""
	}
	return s.server.URL
}

// Close shuts down the server.
func (s *MockLLMServer) Close() {
	if s.server != nil {
		s.server.Close()
		s.server = nil
	}
}

// LastRequestBody returns a copy of the raw request body from the most recent chat request.
// Tests can json.Unmarshal into openai.ChatCompletionRequest to assert on request fields.
func (s *MockLLMServer) LastRequestBody() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastRequest == nil {
		return nil
	}
	out := make([]byte, len(s.lastRequest))
	copy(out, s.lastRequest)
	return out
}

func (s *MockLLMServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" && r.URL.Path != "/v1/chat/completions" {
		slog.Error("mock server: not found", "path", r.URL.Path)
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		slog.Error("mock server: method not allowed", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("mock server: failed to read request body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.lastRequest = body
	s.mu.Unlock()

	var reqBody struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		slog.Error("mock server: failed to unmarshal request body", "error", err)
		reqBody.Stream = false
	}

	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	if cfg.noChoices {
		s.serveNoChoices(w)
		return
	}
	if reqBody.Stream && cfg.streamContentAndToolCalls != nil {
		s.serveStreamWithToolCalls(w, cfg.streamContentAndToolCalls)
		return
	}
	if reqBody.Stream && len(cfg.streamChunks) > 0 {
		s.serveStream(w, cfg)
		return
	}
	s.serveJSON(w, cfg)
}

func (s *MockLLMServer) serveNoChoices(w http.ResponseWriter) {
	resp := map[string]any{
		"id":      "x",
		"object":  "chat.completion",
		"created": 0,
		"model":   "m",
		"choices": []any{},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("mock server: failed to encode no-choices response", "error", err)
		return
	}
}

func (s *MockLLMServer) serveJSON(w http.ResponseWriter, cfg mockLLMServerConfig) {
	toolCalls := make([]map[string]any, 0, len(cfg.toolCalls))
	for _, tc := range cfg.toolCalls {
		toolCalls = append(toolCalls, map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": tc.Args,
			},
		})
	}

	msg := map[string]any{
		"role":    "assistant",
		"content": cfg.content,
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	resp := map[string]any{
		"id":      "cmpl_mock",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock",
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("mock server: failed to encode JSON response", "error", err)
		return
	}
}

func (s *MockLLMServer) serveStream(w http.ResponseWriter, cfg mockLLMServerConfig) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	for _, chunk := range cfg.streamChunks {
		delta := map[string]any{"content": chunk}
		evt := map[string]any{
			"id":      "x",
			"object":  "chat.completion.chunk",
			"created": 0,
			"model":   "m",
			"choices": []map[string]any{
				{"index": 0, "delta": delta},
			},
		}
		data, err := json.Marshal(evt)
		if err != nil {
			slog.Error("mock server: failed to marshal stream chunk", "error", err)
			return
		}
		if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
			slog.Error("mock server: failed to write stream chunk", "error", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	finishEvt := map[string]any{
		"id":      "x",
		"object":  "chat.completion.chunk",
		"created": 0,
		"model":   "m",
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"},
		},
	}
	data, err := json.Marshal(finishEvt)
	if err != nil {
		slog.Error("mock server: failed to marshal stream finish event", "error", err)
		return
	}
	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		slog.Error("mock server: failed to write stream finish event", "error", err)
		return
	}
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
		slog.Error("mock server: failed to write stream done marker", "error", err)
		return
	}
}

func (s *MockLLMServer) serveStreamWithToolCalls(w http.ResponseWriter, cfg *struct {
	Content   string
	ToolCalls []mockLLMToolCall
}) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Content chunk
	if cfg.Content != "" {
		evt := map[string]any{
			"id":      "x",
			"object":  "chat.completion.chunk",
			"created": 0,
			"model":   "m",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": cfg.Content}},
			},
		}
		data, err := json.Marshal(evt)
		if err != nil {
			slog.Error("mock server: failed to marshal stream content chunk", "error", err)
			return
		}
		if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
			slog.Error("mock server: failed to write stream content chunk", "error", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	// Tool calls chunk
	if len(cfg.ToolCalls) > 0 {
		toolCallsDelta := make([]map[string]any, len(cfg.ToolCalls))
		for i, tc := range cfg.ToolCalls {
			toolCallsDelta[i] = map[string]any{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Args,
				},
			}
		}
		evt := map[string]any{
			"id":      "x",
			"object":  "chat.completion.chunk",
			"created": 0,
			"model":   "m",
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"tool_calls": toolCallsDelta}},
			},
		}
		data, err := json.Marshal(evt)
		if err != nil {
			slog.Error("mock server: failed to marshal stream tool-calls chunk", "error", err)
			return
		}
		if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
			slog.Error("mock server: failed to write stream tool-calls chunk", "error", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}

	// Finish
	finishEvt := map[string]any{
		"id":      "x",
		"object":  "chat.completion.chunk",
		"created": 0,
		"model":   "m",
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"},
		},
	}
	data, err := json.Marshal(finishEvt)
	if err != nil {
		slog.Error("mock server: failed to marshal stream-with-tool-calls finish event", "error", err)
		return
	}
	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		slog.Error("mock server: failed to write stream-with-tool-calls finish event", "error", err)
		return
	}
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
		slog.Error("mock server: failed to write stream-with-tool-calls done marker", "error", err)
		return
	}
}
