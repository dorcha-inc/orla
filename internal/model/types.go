// Package model provides model integration for Orla Agent Mode (RFC 4).
package model

import (
	"context"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// Message represents a chat message in a conversation
type Message struct {
	Role     MessageRole `json:"role"`                // "user", "assistant", "system", or "tool"
	Content  string      `json:"content"`             // Message content
	ToolName string      `json:"tool_name,omitempty"` // Tool name (required when role is "tool")
}

// ToolCallWithID represents a tool invocation request from the model.
// It embeds mcp.CallToolParams for MCP compatibility, and adds an ID
// for tracking in the agent loop (to match results back to calls).
type ToolCallWithID struct {
	ID                string `json:"id"` // Unique identifier for this tool call
	McpCallToolParams mcp.CallToolParams
}

// ToolResultWithID represents the result of a tool execution.
// It embeds mcp.CallToolResult for MCP compatibility, and adds an ID
// to match back to the original ToolCall.
type ToolResultWithID struct {
	ID                string `json:"id"` // Tool call ID this result corresponds to
	McpCallToolResult mcp.CallToolResult
}

// Response represents a model response
type Response struct {
	Content     string             `json:"content"`      // Text content from the model
	ToolCalls   []ToolCallWithID   `json:"tool_calls"`   // Tool calls requested by the model
	ToolResults []ToolResultWithID `json:"tool_results"` // Tool results returned by the model
}

// Provider is the interface that all model providers must implement
type Provider interface {
	// Name returns the provider name (e.g., "ollama", "openai", "anthropic")
	Name() string

	// Chat sends a chat request to the model and returns the response
	// messages: conversation history
	// tools: available tools (for tool calling) - uses mcp.Tool for MCP compatibility
	// stream: if true, stream responses via the returned channel
	Chat(ctx context.Context, messages []Message, tools []*mcp.Tool, stream bool) (*Response, <-chan string, error)

	// EnsureReady ensures the model provider is ready (e.g., starts Ollama if needed)
	// Returns an error if the provider cannot be made ready
	EnsureReady(ctx context.Context) error
}

// StreamWriter is an interface for writing streaming responses
type StreamWriter interface {
	io.Writer
	Flush() error
}
