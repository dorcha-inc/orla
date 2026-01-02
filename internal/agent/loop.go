// Package agent implements the agent loop and MCP client for Orla Agent Mode (RFC 4).
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/dorcha-inc/orla/internal/tui"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// MCPClient is an interface for MCP client operations used by the agent loop
type MCPClient interface {
	ListTools(ctx context.Context) ([]*mcp.Tool, error)
	CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

// Loop orchestrates the agent execution flow
type Loop struct {
	client   MCPClient
	provider model.Provider
	cfg      *config.OrlaConfig
}

// NewLoop creates a new agent loop
func NewLoop(client MCPClient, provider model.Provider, cfg *config.OrlaConfig) *Loop {
	return &Loop{
		client:   client,
		provider: provider,
		cfg:      cfg,
	}
}

// StreamHandler is a function that handles streaming events
type StreamHandler func(event model.StreamEvent) error

// Execute runs a single agent execution cycle
// It implements the agent loop from RFC 4 Section 4.4:
// 1. Receive user prompt
// 2. Send prompt and available tools to the model
// 3. Receive model response (may include tool calls)
// 4. Execute tool calls via MCP
// 5. Return tool results to the model
// 6. Receive final response from the model
// 7. Stream response to user (if streaming and handler provided)
//
// If streamHandler is provided and streaming is enabled, it will be called for each chunk.
// The stream will be consumed before checking for tool calls, ensuring the response is complete.
func (l *Loop) Execute(ctx context.Context, prompt string, messages []model.Message, stream bool, streamHandler StreamHandler) (*model.Response, error) {
	if stream && streamHandler == nil {
		return nil, fmt.Errorf("stream handler is required when streaming is enabled")
	}

	// Get available tools from the MCP server
	tools, err := l.client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	zap.L().Debug("Agent loop starting",
		zap.String("prompt", prompt),
		zap.Int("tool_count", len(tools)),
		zap.Int("message_count", len(messages)))

	// Build conversation messages
	conversation := make([]model.Message, len(messages))
	copy(conversation, messages)

	// Add the new user prompt
	if prompt != "" {
		conversation = append(conversation, model.Message{
			Role:    model.MessageRoleUser,
			Content: prompt,
		})
	}

	// Convert MCP tools to model format
	mcpTools := make([]*mcp.Tool, len(tools))
	copy(mcpTools, tools)

	// Maximum number of tool call iterations to prevent infinite loops
	maxIterations := l.cfg.MaxToolCalls
	if maxIterations <= 0 {
		maxIterations = 10 // Default
	}

	// Agent loop: iterate until we get a final response without tool calls
	for iteration := 0; iteration < maxIterations; iteration++ {
		tui.Progress(fmt.Sprintf("Processing request (iteration %d)", iteration+1))

		zap.L().Debug("Agent loop iteration",
			zap.Int("iteration", iteration+1),
			zap.Int("max_iterations", maxIterations))

		// Send prompt and tools to the model
		response, streamCh, err := l.provider.Chat(ctx, conversation, mcpTools, stream)

		if err != nil {
			return nil, fmt.Errorf("model chat failed: %w", err)
		}

		// Check if response is nil (shouldn't happen, but be safe)
		if response == nil {
			return nil, fmt.Errorf("received nil response from model")
		}

		if stream && streamCh == nil {
			return nil, fmt.Errorf("stream channel is nil but streaming is enabled")
		}

		// If we have a stream channel, consume it before checking for tool calls
		// This ensures the response object is fully populated
		if streamCh != nil {
			tui.ProgressSuccess(fmt.Sprintf("Iteration %d completed, stream started.", iteration+1))
			for event := range streamCh {
				if err := streamHandler(event); err != nil {
					return nil, fmt.Errorf("stream handler error: %w", err)
				}
			}
			// Stream is now complete, response should be fully populated
		}

		// If there are no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			// Final response - return it
			return response, nil
		}

		// Show tool calls being executed
		if len(response.ToolCalls) > 0 {
			toolNames := make([]string, len(response.ToolCalls))
			for i, tc := range response.ToolCalls {
				toolNames[i] = tc.McpCallToolParams.Name
			}

			tui.Progress(fmt.Sprintf("Executing orla mcp tools: %s", strings.Join(toolNames, ", ")))
		}

		// Execute tool calls
		toolResults := l.executeToolCalls(ctx, response.ToolCalls)

		tui.ProgressSuccess("")

		// Add tool results to conversation for next iteration
		// Format: assistant message (with content if any), then tool results as tool messages
		// Ollama supports "tool" role messages with tool_name and content fields
		if response.Content != "" {
			conversation = append(conversation, model.Message{
				Role:    model.MessageRoleAssistant,
				Content: response.Content,
			})
		}

		// Add tool results as tool messages (one per tool call)
		// Each tool result becomes a separate message with role "tool"
		// Ollama matches tool results to tool calls by tool_name
		for _, result := range toolResults {
			// Find the corresponding tool call to get the tool name
			var toolName string
			for _, toolCall := range response.ToolCalls {
				if toolCall.ID == result.ID {
					toolName = toolCall.McpCallToolParams.Name
					break
				}
			}

			// If we couldn't find the tool name, log a warning but continue
			if toolName == "" {
				zap.L().Warn("Could not find tool name for tool result",
					zap.String("result_id", result.ID))
				// Skip this result if we can't identify the tool
				continue
			}

			// Format the tool result content
			resultContent := formatToolResult(result)

			conversation = append(conversation, model.Message{
				Role:     model.MessageRoleTool,
				ToolName: toolName,
				Content:  resultContent,
			})
		}

		// Continue to next iteration to get final response
		// (Don't return here - we need the model to synthesize a final response)
		// Note: We don't stream subsequent iterations (only the first user prompt)
	}

	// If we've exhausted iterations, return an error
	return nil, fmt.Errorf("maximum tool call iterations (%d) reached", maxIterations)
}

// executeToolCalls executes a list of tool calls via MCP and returns the results
func (l *Loop) executeToolCalls(ctx context.Context, toolCalls []model.ToolCallWithID) []model.ToolResultWithID {
	zap.L().Debug("Executing tool calls",
		zap.Int("count", len(toolCalls)))

	toolResults := make([]model.ToolResultWithID, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		// Execute tool call via MCP
		result, err := l.client.CallTool(ctx, &toolCall.McpCallToolParams)
		if err != nil {
			zap.L().Warn("Tool call failed",
				zap.String("tool", toolCall.McpCallToolParams.Name),
				zap.Error(err))

			// Create error result
			toolResults = append(toolResults, model.ToolResultWithID{
				ID: toolCall.ID,
				McpCallToolResult: mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("Tool call failed: %v", err),
						},
					},
				},
			})
			continue
		}

		// Add successful result
		toolResults = append(toolResults, model.ToolResultWithID{
			ID:                toolCall.ID,
			McpCallToolResult: *result,
		})

		zap.L().Debug("Tool call completed",
			zap.String("tool", toolCall.McpCallToolParams.Name),
			zap.Bool("is_error", result.IsError))
	}

	return toolResults
}

// formatToolResult formats a single tool result as text for the model
// Returns just the content text - Ollama will match results to calls by tool_name
func formatToolResult(result model.ToolResultWithID) string {
	var text string

	// Extract text content from result
	for _, content := range result.McpCallToolResult.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			if text != "" {
				text += "\n"
			}
			text += textContent.Text
		}
	}

	// If no content was found, provide a default message
	if text == "" {
		if result.McpCallToolResult.IsError {
			text = "Tool execution failed"
		} else {
			text = "Tool executed successfully"
		}
	}

	return text
}
