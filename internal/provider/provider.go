// Package provider integrates the backends orla dispatches to.
//
// Two backend kinds are supported:
//
//   - LLM backends speak OpenAI-compatible chat completions. The
//     openAIProvider in openai.go is the canonical implementation.
//
//   - Tool backends speak a kind-specific JSON RPC over HTTP. Each
//     ToolKind is implemented in its own subpackage under provider/.
//
// The Backend interface is the common identity both kinds share.
// Scheduler machinery such as concurrency caps, rate limits, and
// telemetry is kind-agnostic and operates on Backend. The proxy layer
// is kind-aware and routes per kind to LLMProvider.Chat or
// ToolProvider.Invoke.
package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
)

// Backend is the kind-agnostic identity shared by every backend the
// scheduler knows about. Concrete providers embed this contract.
type Backend interface {
	// Name returns the backend's registered name. It matches the
	// `name` column in the backends table and is stable across the
	// process lifetime.
	Name() string
}

// LLMProvider is implemented by OpenAI-compatible chat backends.
type LLMProvider interface {
	Backend

	// ModelID returns the resolved model identifier without the
	// provider prefix. The proxy overwrites the request's Model
	// field with this before dispatch. The developer's value is
	// advisory only.
	ModelID() string

	// Chat sends a non-streaming chat completion.
	Chat(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)

	// ChatStream opens a streaming chat completion. The caller must
	// Close the stream when done. Concurrency slots are held by the
	// scheduler until that happens.
	ChatStream(ctx context.Context, params openai.ChatCompletionNewParams) *ssestream.Stream[openai.ChatCompletionChunk]
}

// ToolProvider is implemented by non-LLM backends, for example
// structure prediction, docking, or ADMET property prediction. Each
// concrete implementation handles one ToolKind.
type ToolProvider interface {
	Backend

	// ToolKind returns the kind of tool this provider serves, such as
	// "structure-prediction". The proxy uses this to validate that
	// the request's kind matches the resolved backend's kind.
	ToolKind() string

	// Invoke dispatches a tool request. The payload schema is
	// kind-specific and opaque to the scheduler. The proxy decodes
	// per kind.
	Invoke(ctx context.Context, req ToolRequest) (*ToolResponse, error)
}

// ToolRequest is the wire-shape envelope the proxy passes to
// ToolProvider.Invoke. Payload is kind-specific JSON. The concrete
// provider decodes it according to its ToolKind.
type ToolRequest struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// ToolResponse is the wire-shape envelope returned from
// ToolProvider.Invoke. Payload is kind-specific.
//
// Usage reports resource consumption for cost accounting. The map
// holds resource_name to amount used. Keys are tool-defined.
// Examples:
//
//	{"gpu_seconds": 5.0}            a GPU-billed tool
//	{"cpu_seconds": 12.3, "calls": 1}  a CPU-billed API
//	{"molecules_docked": 200}       per-unit billed
//
// Orla looks up matching rates on the backend record and computes
// cost as the dot product of usage and rates.
//
// CostUSD lets a tool that already knows its own price short-circuit
// the rates lookup. If non-nil, orla uses this value directly and
// ignores Usage for cost accounting. Usage is still recorded for
// observability.
//
// Metadata is opaque diagnostic data the tool wants to surface.
type ToolResponse struct {
	Payload  json.RawMessage    `json:"payload"`
	Usage    map[string]float64 `json:"usage,omitempty"`
	CostUSD  *float64           `json:"cost_usd,omitempty"`
	Metadata map[string]any     `json:"metadata,omitempty"`
}

// ParseModelID splits a backend model id of the form "provider:model"
// into its (provider, model) parts. If no colon is present, the input
// is treated as a model name with an empty provider.
func ParseModelID(s string) (provider, model string) {
	before, after, ok := strings.Cut(s, ":")
	if !ok {
		return "", s
	}
	return before, after
}
