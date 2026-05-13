// Package orla provides a public Go client library for Orla server.

package orla

import (
	"context"
	"encoding/json"
	"fmt"
)

// toolPayload is the wire shape sent to the daemon for a single tool definition.
// Mirrors internal/model.Tool's JSON encoding.
type toolPayload struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolSchema is a JSON-serializable object (e.g. for tool input/output).
type ToolSchema map[string]any

// ToolRunner runs a tool: input from the model, result back to the model.
// Return a ToolResult with OutputValues (and optionally Error/IsError for tool-level failures).
// ID and Name are filled in by the agent; the runner only sets OutputValues, Error, IsError.
// Returning a non-nil error is treated as IsError true with Error set to err.Error().
type ToolRunner func(ctx context.Context, input ToolSchema) (*ToolResult, error)

// Tool defines a single tool: name, description, schemas, and runner.
type Tool struct {
	Name         string
	Description  string
	InputSchema  ToolSchema
	OutputSchema ToolSchema
	Run          ToolRunner
}

// NewTool returns a Tool. run must be non-nil.
func NewTool(name, description string, inputSchema, outputSchema ToolSchema, run ToolRunner) (*Tool, error) {
	if run == nil {
		return nil, fmt.Errorf("tool runner cannot be nil")
	}
	return &Tool{
		Name:         name,
		Description:  description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Run:          run,
	}, nil
}

// ToolRunnerFromSchema wraps a simple (ToolSchema, error) function as a ToolRunner.
// Use this when you don't need to return tool-level Error/IsError; a returned Go error becomes result.IsError.
func ToolRunnerFromSchema(fn func(ctx context.Context, input ToolSchema) (ToolSchema, error)) ToolRunner {
	return func(ctx context.Context, input ToolSchema) (*ToolResult, error) {
		out, err := fn(ctx, input)
		if err != nil {
			return &ToolResult{Error: err.Error(), IsError: true}, nil
		}
		if out == nil {
			out = ToolSchema{}
		}
		return &ToolResult{OutputValues: out}, nil
	}
}

// toWire returns the wire payload sent to the daemon for this tool.
func (t *Tool) toWire() (*toolPayload, error) {
	out := &toolPayload{Name: t.Name, Description: t.Description}
	if t.InputSchema != nil {
		params, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshal tool %q input schema: %w", t.Name, err)
		}
		out.Parameters = params
	}
	return out, nil
}

// ToolCall is one tool invocation from the agent.
type ToolCall struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	InputArguments ToolSchema `json:"input_arguments"`
}

// ToolResult is the result of running one tool call.
type ToolResult struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	OutputValues ToolSchema `json:"output_values"`
	Error        string     `json:"error,omitempty"`
	IsError      bool       `json:"is_error,omitempty"`
}

// toolCallWire is the wire shape returned by the daemon for one tool call.
// Mirrors internal/model.ToolCall's JSON encoding.
type toolCallWire struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// NewToolCallFromRawToolCall converts a raw tool call from an InferenceResponse to a ToolCall.
// data is an element of the array InferenceResponse.ToolCalls.
func NewToolCallFromRawToolCall(rawToolCall RawToolCall) (*ToolCall, error) {
	if len(rawToolCall) == 0 {
		return nil, fmt.Errorf("rawToolCall is empty")
	}
	var w toolCallWire
	if err := json.Unmarshal(rawToolCall, &w); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tool call: %w", err)
	}
	args := ToolSchema{}
	if len(w.Arguments) > 0 {
		if err := json.Unmarshal(w.Arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool call arguments: %w", err)
		}
	}
	return &ToolCall{ID: w.ID, Name: w.Name, InputArguments: args}, nil
}

// ToMessage returns a tool-result message to append to the conversation.
func (r ToolResult) ToMessage() (*Message, error) {
	message := &Message{
		Role:       "tool",
		ToolCallID: r.ID,
		ToolName:   r.Name,
	}

	if r.IsError {
		toolPrefix := "tool execution error"
		if r.Error != "" {
			toolPrefix += ": " + r.Error
		}
		message.Content = toolPrefix
		return message, nil
	}

	if len(r.OutputValues) == 0 {
		return nil, fmt.Errorf("output values are empty")
	}

	outputValues, err := json.Marshal(r.OutputValues)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal output values: %w", err)
	}

	message.Content = string(outputValues)
	return message, nil
}
