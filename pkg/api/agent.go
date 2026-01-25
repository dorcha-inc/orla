package orla

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AgentExecutor provides high-level agent execution with tool support
// Tools are handled client-side via MCP
// The daemon handles inference; the client handles the agent loop with tools
// The daemon reads orla.yaml and resolves server names from agent profiles automatically.
type AgentExecutor struct {
	client *Client
}

// NewAgentExecutor creates a new agent executor
// The daemon reads orla.yaml and resolves server names from agent profiles automatically.
func NewAgentExecutor(daemonURL string) *AgentExecutor {
	return &AgentExecutor{
		client: NewClient(daemonURL),
	}
}

// AgentExecuteRequest represents a request to execute an agent
type AgentExecuteRequest struct {
	ProfileName string      `json:"profile_name"`
	Prompt      string      `json:"prompt"`
	Messages    []Message   `json:"messages,omitempty"` // Conversation history
	Tools       []*mcp.Tool `json:"tools,omitempty"`    // Available tools (from MCP)
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Stream      bool        `json:"stream,omitempty"`
}

// AgentExecuteResponse represents the response from agent execution
type AgentExecuteResponse struct {
	Success  bool          `json:"success"`
	Response *TaskResponse `json:"response,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// Execute executes a single agent inference call
// This is a single-turn execution. For multi-turn with tool calling,
// use ExecuteWithTools which handles the agent loop client-side.
func (e *AgentExecutor) Execute(ctx context.Context, req *AgentExecuteRequest) (*TaskResponse, error) {
	url := fmt.Sprintf("%s/api/v1/agent/execute", e.client.baseURL)

	// Convert public API types to internal request format
	internalReq := map[string]interface{}{
		"profile_name": req.ProfileName,
		"prompt":       req.Prompt,
		"max_tokens":   req.MaxTokens,
		"stream":       req.Stream,
	}

	// Convert messages
	if len(req.Messages) > 0 {
		messages := make([]map[string]interface{}, len(req.Messages))
		for i, msg := range req.Messages {
			messages[i] = map[string]interface{}{
				"role":    msg.Role,
				"content": msg.Content,
			}
		}
		internalReq["messages"] = messages
	}

	// Tools are already in MCP format, just include them directly
	if len(req.Tools) > 0 {
		internalReq["tools"] = req.Tools
	}

	body, err := json.Marshal(internalReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create agent execute request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := e.client.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agent execute request failed: %w", err)
	}

	defer LogDeferredError(httpResp.Body.Close)

	if httpResp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return nil, fmt.Errorf("agent execute failed with status %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var execResp AgentExecuteResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&execResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !execResp.Success {
		return nil, fmt.Errorf("agent execution failed: %s", execResp.Error)
	}

	return execResp.Response, nil
}

// ExecuteWithTools executes an agent with tool support, handling the full agent loop
// This requires client-side MCP connection for tools
// The daemon handles inference; the client handles tool execution via MCP
// Returns the final response after all tool calls are executed
func (e *AgentExecutor) ExecuteWithTools(
	ctx context.Context,
	profileName string,
	prompt string,
	mcpSession *mcp.ClientSession,
	maxIterations int,
	onIteration func(iteration int, response *TaskResponse) error,
) (*TaskResponse, error) {
	if maxIterations <= 0 {
		maxIterations = 10 // Default
	}

	// Get tools from MCP session
	listResult, err := mcpSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Filter out invalid tools
	validTools := make([]*mcp.Tool, 0, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		if tool != nil && tool.Name != "" {
			validTools = append(validTools, tool)
		}
	}

	var conversation []Message

	// Agent loop: iterate until we get a final response without tool calls
	for iteration := 0; iteration < maxIterations; iteration++ {
		// Build request
		req := &AgentExecuteRequest{
			ProfileName: profileName,
			Prompt:      prompt,
			Messages:    conversation,
			Tools:       validTools,
		}

		// Execute inference via daemon
		response, err := e.Execute(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("inference failed on iteration %d: %w", iteration+1, err)
		}

		// Call iteration callback
		if onIteration != nil {
			if err := onIteration(iteration+1, response); err != nil {
				return nil, fmt.Errorf("iteration callback error: %w", err)
			}
		}

		// Add assistant message to conversation
		if response.Content != "" {
			conversation = append(conversation, Message{
				Role:    "assistant",
				Content: response.Content,
			})
		}

		// If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			return response, nil
		}

		// Execute tool calls via MCP client
		for _, toolCall := range response.ToolCalls {
			// Convert tool call from JSON format to MCP format
			toolCallMap, ok := toolCall.(map[string]any)
			if !ok {
				continue
			}

			// Extract tool call parameters
			toolName, ok := toolCallMap["name"].(string)
			if !ok || toolName == "" {
				continue
			}

			arguments, ok := toolCallMap["arguments"].(map[string]any)
			if !ok || arguments == nil {
				arguments = make(map[string]any)
			}

			// Call tool via MCP session
			params := &mcp.CallToolParams{
				Name:      toolName,
				Arguments: arguments,
			}
			result, err := mcpSession.CallTool(ctx, params)
			if err != nil {
				return nil, fmt.Errorf("tool call failed for %s: %w", toolName, err)
			}

			// Extract content from MCP result
			var content string
			if result != nil && len(result.Content) > 0 {
				// Convert MCP content to string
				for _, c := range result.Content {
					if textContent, ok := c.(*mcp.TextContent); ok {
						content += textContent.Text
					} else if imageContent, ok := c.(*mcp.ImageContent); ok {
						// For images, include a reference
						if len(imageContent.Data) > 0 {
							content += fmt.Sprintf("[Image: %d bytes]", len(imageContent.Data))
						}
					} else {
						// Fallback: try to marshal as JSON
						if jsonBytes, marshalErr := json.Marshal(c); marshalErr == nil {
							content += string(jsonBytes)
						}
					}
				}
			}

			conversation = append(conversation, Message{
				Role:    "tool",
				Content: content,
			})
		}

		// Clear prompt for next iteration (use empty string to continue conversation)
		prompt = ""
	}

	return nil, fmt.Errorf("max iterations (%d) reached", maxIterations)
}
