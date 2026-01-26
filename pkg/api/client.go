// Package orla provides a public Go client library for the Orla Agentic Serving Layer daemon API (RFC 5).
//
// This package enables external code to interact with the Orla daemon for:
// - Workflow execution and coordination
// - Shared context management
// - Multi-agent experiments
//
// Example usage:
//
//	client := api.NewClient("http://localhost:8081")
//	execID, err := client.StartWorkflow(ctx, "story_finishing_game")
//	task, taskIndex, complete, err := client.GetNextTask(ctx, execID)
//	response, err := client.ExecuteTask(ctx, execID, taskIndex, prompt, maxTokens)
//	err := client.CompleteTask(ctx, execID, taskIndex, response)
package orla

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is the public API client for the Orla daemon
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new daemon API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// Health checks the health of the daemon
func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check health: %w", err)
	}
	defer LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// StartWorkflowRequest represents a workflow start request
type StartWorkflowRequest struct {
	WorkflowName string `json:"workflow_name"`
}

// StartWorkflowResponse represents a workflow start response
type StartWorkflowResponse struct {
	ExecutionID string `json:"execution_id"`
	Error       string `json:"error,omitempty"`
}

// StartWorkflow starts a workflow execution on the daemon
func (c *Client) StartWorkflow(ctx context.Context, workflowName string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/workflow/start", c.baseURL)

	req := StartWorkflowRequest{
		WorkflowName: workflowName,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create workflow start request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to start workflow: %w", err)
	}
	defer LogDeferredError(httpResp.Body.Close)

	if httpResp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %w", err)
		}
		return "", fmt.Errorf("workflow start returned status %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var workflowResp StartWorkflowResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&workflowResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if workflowResp.Error != "" {
		return "", fmt.Errorf("workflow start failed: %s", workflowResp.Error)
	}

	return workflowResp.ExecutionID, nil
}

// GetNextTaskResponse represents the response from getting the next task
type GetNextTaskResponse struct {
	Task      *WorkflowTask `json:"task"`
	TaskIndex int           `json:"task_index"`
	Complete  bool          `json:"complete"`
	LLMServer string        `json:"llm_server,omitempty"` // Resolved server name from daemon
	Error     string        `json:"error,omitempty"`
}

// WorkflowTask represents a workflow task
// This matches the structure returned by the daemon API
type WorkflowTask struct {
	// AgentProfile is the name of the agent profile to use for this task
	AgentProfile string `json:"agent_profile"`
	// LLMServer is an optional override for the LLM server configuration
	LLMServer string `json:"llm_server,omitempty"`
	// Turn is the turn number for multi-agent coordination (1-based)
	Turn int `json:"turn,omitempty"`
	// Prompt is the prompt or prompt template for this task
	Prompt string `json:"prompt,omitempty"`
	// UseContext indicates whether to use previous task outputs as context
	UseContext bool `json:"use_context,omitempty"`
}

// GetNextTask retrieves the next task to execute from a workflow
// Returns the task, task index, completion status, and resolved LLM server name
func (c *Client) GetNextTask(ctx context.Context, executionID string) (*WorkflowTask, int, bool, string, error) {
	url := fmt.Sprintf("%s/api/v1/workflow/task/next?execution_id=%s", c.baseURL, executionID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, false, "", fmt.Errorf("failed to create get next task request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, false, "", fmt.Errorf("get next task request failed: %w", err)
	}
	defer LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, 0, false, "", fmt.Errorf("failed to read response body: %w", err)
		}
		return nil, 0, false, "", fmt.Errorf("get next task failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var taskResp GetNextTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, 0, false, "", fmt.Errorf("failed to decode response: %w", err)
	}

	if taskResp.Error != "" {
		return nil, 0, false, "", fmt.Errorf("get next task failed: %s", taskResp.Error)
	}

	return taskResp.Task, taskResp.TaskIndex, taskResp.Complete, taskResp.LLMServer, nil
}

// ExecuteTaskRequest represents a task execution request
type ExecuteTaskRequest struct {
	ExecutionID string `json:"execution_id"`
	TaskIndex   int    `json:"task_index"`
	Prompt      string `json:"prompt"`
	MaxTokens   *int   `json:"max_tokens,omitempty"` // Optional: maximum tokens to generate
}

// ExecuteTaskResponse represents a task execution response
type ExecuteTaskResponse struct {
	Success  bool          `json:"success"`
	Response *TaskResponse `json:"response,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// TaskResponse represents the response from a task execution
type TaskResponse struct {
	Content   string `json:"content"`
	Thinking  string `json:"thinking,omitempty"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

// ExecuteTask executes a workflow task on the daemon
// maxTokens is optional (pass 0 or negative to omit)
func (c *Client) ExecuteTask(ctx context.Context, executionID string, taskIndex int, prompt string, maxTokens int) (*TaskResponse, error) {
	url := fmt.Sprintf("%s/api/v1/workflow/task/execute", c.baseURL)

	reqBody := ExecuteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   taskIndex,
		Prompt:      prompt,
	}
	if maxTokens > 0 {
		reqBody.MaxTokens = &maxTokens
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create execute task request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute task request failed: %w", err)
	}
	defer LogDeferredError(httpResp.Body.Close)

	if httpResp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return nil, fmt.Errorf("execute task failed with status %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var execResp ExecuteTaskResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&execResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !execResp.Success {
		return nil, fmt.Errorf("task execution failed: %s", execResp.Error)
	}

	return execResp.Response, nil
}

// CompleteTaskRequest represents a task completion request
type CompleteTaskRequest struct {
	ExecutionID string        `json:"execution_id"`
	TaskIndex   int           `json:"task_index"`
	Response    *TaskResponse `json:"response"`
}

// CompleteTaskResponse represents a task completion response
type CompleteTaskResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// CompleteTask marks a task as complete and reports the response to the daemon
func (c *Client) CompleteTask(ctx context.Context, executionID string, taskIndex int, response *TaskResponse) error {
	url := fmt.Sprintf("%s/api/v1/workflow/task/complete", c.baseURL)

	reqBody := CompleteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   taskIndex,
		Response:    response,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create complete task request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("complete task request failed: %w", err)
	}
	defer LogDeferredError(httpResp.Body.Close)

	if httpResp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		return fmt.Errorf("complete task failed with status %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var completeResp CompleteTaskResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&completeResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !completeResp.Success {
		return fmt.Errorf("complete task failed: %s", completeResp.Error)
	}

	return nil
}

// GetContextResponse represents the response from getting context
type GetContextResponse struct {
	Messages []Message `json:"messages"`
	Error    string    `json:"error,omitempty"`
}

// Message represents a chat message (public API version)
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GetContext retrieves shared context from the daemon for a given LLM server
func (c *Client) GetContext(ctx context.Context, serverName string) ([]Message, error) {
	url := fmt.Sprintf("%s/api/v1/context/%s", c.baseURL, serverName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get context request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get context request failed: %w", err)
	}
	defer LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		return nil, fmt.Errorf("get context failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var contextResp GetContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&contextResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if contextResp.Error != "" {
		return nil, fmt.Errorf("get context failed: %s", contextResp.Error)
	}

	return contextResp.Messages, nil
}

// SyncContextRequest represents a context sync request
type SyncContextRequest struct {
	ServerName string    `json:"server_name"`
	Messages   []Message `json:"messages"`
}

// SyncContextResponse represents a context sync response
type SyncContextResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SyncContext syncs local context with the daemon
func (c *Client) SyncContext(ctx context.Context, serverName string, messages []Message) error {
	url := fmt.Sprintf("%s/api/v1/context/sync", c.baseURL)

	reqBody := SyncContextRequest{
		ServerName: serverName,
		Messages:   messages,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create context sync request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("context sync request failed: %w", err)
	}
	defer LogDeferredError(httpResp.Body.Close)

	if httpResp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		return fmt.Errorf("context sync failed with status %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var syncResp SyncContextResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&syncResp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !syncResp.Success {
		return fmt.Errorf("context sync failed: %s", syncResp.Error)
	}

	return nil
}
