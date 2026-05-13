// Package model provides model integration for Orla Agent Mode (RFC 4).
package model

import (
	"context"
	"encoding/json"
	"io"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

func (r MessageRole) String() string {
	return string(r)
}

// Message represents a chat message in a conversation.
// For role "tool" i.e tool calls, set both ToolCallID and ToolName when building messages.
// The OpenAI API uses ToolCallID and the Ollama API uses ToolName. The providers ignore the field they do not need.
type Message struct {
	// Role of the message
	Role MessageRole `json:"role"`
	// Content of the message
	Content string `json:"content"`
	// ToolName is used by the Ollama API
	ToolName string `json:"tool_name,omitempty"`
	// ToolCallID is used by the OpenAI API and vLLM
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls carries the tool calls from an assistant message so they can be
	// replayed in the conversation history on the next turn.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Tool describes a function-style tool the model can call. Mirrors the OpenAI
// wire shape; Parameters is a JSON Schema passed through verbatim.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a tool invocation request from the model. Arguments is the
// JSON-encoded argument object as returned by the upstream API; Orla passes
// it through without introspecting it.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ResponseMetrics holds latency, token, and cost data for an inference call.
//
// Pointer fields are mode-specific or depend on optional configuration — nil means
// "not applicable" or "not available", which is distinct from a measured zero.
// Bare-value fields are always populated by the scheduler or backend in every mode.
type ResponseMetrics struct {
	// Streaming-only. Nil for non-streaming requests.
	TTFTMs *int64 `json:"ttft_ms,omitempty"`
	TPOTMs *int64 `json:"tpot_ms,omitempty"`

	// Reported by the backend in both modes.
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`

	// Always populated by the scheduler.
	QueueWaitMs         int64 `json:"queue_wait_ms,omitempty"`
	SchedulerDecisionMs int64 `json:"scheduler_decision_ms,omitempty"`
	DispatchMs          int64 `json:"dispatch_ms,omitempty"`

	// Non-streaming only. Nil for streaming requests.
	BackendLatencyMs *int64 `json:"backend_latency_ms,omitempty"`

	// Nil when the backend has no CostModel configured.
	EstimatedCostUSD *float64 `json:"estimated_cost_usd,omitempty"`
}

// SchedulingPolicy controls how the server picks the next stage queue on a backend.
type SchedulingPolicy string

const (
	// SchedulingPolicyFCFS is first-come-first-served stage scheduling.
	SchedulingPolicyFCFS SchedulingPolicy = "fcfs"
	// SchedulingPolicyPriority picks the stage with the highest-priority head request.
	SchedulingPolicyPriority SchedulingPolicy = "priority"
)

// RequestSchedulingPolicy controls how requests within a single stage queue are ordered.
type RequestSchedulingPolicy string

const (
	// RequestSchedulingPolicyFCFS processes requests in arrival order (default).
	RequestSchedulingPolicyFCFS RequestSchedulingPolicy = "fcfs"
	// RequestSchedulingPolicyPriority processes the highest-priority request first.
	RequestSchedulingPolicyPriority RequestSchedulingPolicy = "priority"
)

// SchedulingHints are optional policy-specific hints attached to an inference request.
type SchedulingHints struct {
	// Priority controls stage-level scheduling (which stage queue to serve next).
	Priority *int `json:"priority,omitempty"`
	// RequestPriority controls request-level scheduling (ordering within a stage queue).
	RequestPriority *int `json:"request_priority,omitempty"`
}

// GetPriority returns the stage-level priority score or 0 when unset.
func (h *SchedulingHints) GetPriority() int {
	if h == nil || h.Priority == nil {
		return 0
	}
	return *h.Priority
}

// GetRequestPriority returns the request-level priority score or 0 when unset.
func (h *SchedulingHints) GetRequestPriority() int {
	if h == nil || h.RequestPriority == nil {
		return 0
	}
	return *h.RequestPriority
}

// Response represents a model response
type Response struct {
	Content   string           `json:"content"`    // Text content from the model
	Thinking  string           `json:"thinking"`   // Thinking trace from the model (if supported)
	ToolCalls []ToolCall       `json:"tool_calls"` // Tool calls requested by the model
	Metrics   *ResponseMetrics `json:"metrics"`    // Response metrics
}

// StructuredOutputOptions requests the model to return content conforming to a JSON Schema.
type StructuredOutputOptions struct {
	Name   string          `json:"name"`             // Required by OpenAI for json_schema response_format
	Strict bool            `json:"strict,omitempty"` // If true, response is guaranteed to conform to the schema
	Schema json.RawMessage `json:"schema"`           // JSON Schema object. The schema must be valid when set
}

// InferenceOptions holds per-request inference settings (agent profile knobs).
// JSON tags match the execute API so the server can embed this in ExecuteRequest.
type InferenceOptions struct {
	// Stream enables streaming (SSE) for the response. false means non-streaming.
	Stream bool `json:"stream,omitempty"`
	// MaxTokens is the maximum number of tokens to generate. A nil value means use the backend default.
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Temperature is the temperature parameter for sampling. A nil value means use the backend default.
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP is the nucleus sampling top_p parameter. A nil value means use the backend default.
	TopP *float64 `json:"top_p,omitempty"`
	// ResponseFormat is the structured output options. A nil value means no structured output.
	ResponseFormat *StructuredOutputOptions `json:"response_format,omitempty"`
	// ChatTemplateKwargs are extra kwargs passed to the chat template renderer
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	// SchedulingPolicy selects stage-level backend queue scheduling behavior.
	SchedulingPolicy SchedulingPolicy `json:"scheduling_policy,omitempty"`
	// RequestSchedulingPolicy selects request-level ordering within a stage queue.
	RequestSchedulingPolicy RequestSchedulingPolicy `json:"request_scheduling_policy,omitempty"`
	// SchedulingHints are optional policy hints for backend queueing.
	SchedulingHints *SchedulingHints `json:"scheduling_hints,omitempty"`
	// ReasoningEffort controls thinking for reasoning-capable models ("high", "medium", "low", "none").
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// Accuracy requests cost-optimized backend selection. When set to a value in [0.0, 1.0],
	// the daemon picks the cheapest backend whose Quality >= this value.
	Accuracy *float64 `json:"accuracy,omitempty"`
	// AccuracyPolicy controls fallback when no backend meets the Accuracy threshold.
	// "prefer" (default): fall back to the cheapest backend with a cost model.
	// "strict": fail the request if no backend qualifies.
	AccuracyPolicy string `json:"accuracy_policy,omitempty"`
}

const (
	AccuracyPolicyPrefer = "prefer"
	AccuracyPolicyStrict = "strict"
)

// GetSchedulingPolicy returns the configured scheduling policy or the FCFS default.
func (o InferenceOptions) GetSchedulingPolicy() SchedulingPolicy {
	if o.SchedulingPolicy == "" {
		return SchedulingPolicyFCFS
	}
	return o.SchedulingPolicy
}

// Provider is the interface that all model providers must implement
type Provider interface {
	// Name returns the provider name (e.g., "openai", "anthropic")
	Name() string

	// Chat sends a chat request to the model with the given inference options and returns the response.
	Chat(ctx context.Context, messages []Message, tools []*Tool, opts InferenceOptions) (*Response, <-chan StreamEvent, error)

	// EnsureReady ensures the model provider is ready (e.g., starts Ollama if needed)
	// Returns an error if the provider cannot be made ready
	EnsureReady(ctx context.Context) error
}

// StreamWriter is an interface for writing streaming responses
type StreamWriter interface {
	io.Writer
	Flush() error
}

// StreamEvent represents a single event in the streaming response
type StreamEvent interface {
	// Type returns the type of stream event
	Type() StreamEventType
}

// StreamEventType represents the type of stream event
type StreamEventType string

const (
	StreamEventTypeContent  StreamEventType = "content"  // Text content chunk
	StreamEventTypeToolCall StreamEventType = "toolcall" // Tool call notification
	StreamEventTypeThinking StreamEventType = "thinking" // Thinking trace chunk
)

// ContentEvent represents a content chunk in the stream
type ContentEvent struct {
	Content string
}

func (e *ContentEvent) Type() StreamEventType {
	return StreamEventTypeContent
}

// ToolCallEvent represents a tool call notification in the stream
type ToolCallEvent struct {
	Name      string
	Arguments map[string]any
}

func (e *ToolCallEvent) Type() StreamEventType {
	return StreamEventTypeToolCall
}

// ThinkingEvent represents a thinking trace chunk in the stream
type ThinkingEvent struct {
	Content string
}

func (e *ThinkingEvent) Type() StreamEventType {
	return StreamEventTypeThinking
}
