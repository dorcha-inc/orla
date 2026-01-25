// Package api provides the HTTP client for communicating with the Agentic Serving Layer daemon (RFC 5).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/dorcha-inc/orla/internal/serving"
	"go.uber.org/zap"
)

// DaemonCoordinator handles communication with the daemon for coordination
// (shared context, workflow management). It does NOT proxy inference requests.
// Actual inference happens locally via providers; the daemon coordinates state.
type DaemonCoordinator struct {
	client *Client
}

// NewDaemonCoordinator creates a new daemon coordinator
func NewDaemonCoordinator(daemonURL string) *DaemonCoordinator {
	return &DaemonCoordinator{
		client: NewClient(daemonURL),
	}
}

// Health checks daemon health
func (c *DaemonCoordinator) Health(ctx context.Context) error {
	return c.client.Health(ctx)
}

// SyncContext syncs the local context with the daemon
// This is called after a task execution to share context updates
func (c *DaemonCoordinator) SyncContext(ctx context.Context, serverName string, messages []model.Message) error {
	reqBody := ContextSyncRequest{
		ServerName: serverName,
		Messages:   messages,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal context sync request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.client.baseURL+"/api/v1/context/sync", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create context sync request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("context sync request failed: %w", err)
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("context sync failed with status %d", resp.StatusCode)
	}

	var syncResp ContextSyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return fmt.Errorf("failed to decode context sync response: %w", err)
	}

	if !syncResp.Success {
		return fmt.Errorf("context sync failed: %s", syncResp.Error)
	}

	zap.L().Debug("Synced context with daemon",
		zap.String("server_name", serverName),
		zap.Int("message_count", len(messages)))

	return nil
}

// GetContext retrieves shared context from the daemon
func (c *DaemonCoordinator) GetContext(ctx context.Context, serverName string) ([]model.Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.client.baseURL+"/api/v1/context/"+serverName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get context request: %w", err)
	}

	resp, err := c.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get context request failed: %w", err)
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get context failed with status %d", resp.StatusCode)
	}

	var contextResp GetContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&contextResp); err != nil {
		return nil, fmt.Errorf("failed to decode get context response: %w", err)
	}

	if contextResp.Error != "" {
		return nil, fmt.Errorf("get context failed: %s", contextResp.Error)
	}

	zap.L().Debug("Retrieved context from daemon",
		zap.String("server_name", serverName),
		zap.Int("message_count", len(contextResp.Messages)))

	return contextResp.Messages, nil
}

// StartWorkflow starts a workflow execution on the daemon
func (c *DaemonCoordinator) StartWorkflow(ctx context.Context, workflowName string) (string, error) {
	resp, err := c.client.StartWorkflow(ctx, workflowName)
	if err != nil {
		return "", err
	}

	if resp.Error != "" {
		return "", fmt.Errorf("failed to start workflow: %s", resp.Error)
	}

	zap.L().Debug("Started workflow via daemon",
		zap.String("workflow_name", workflowName),
		zap.String("execution_id", resp.ExecutionID))

	return resp.ExecutionID, nil
}

// GetNextTask retrieves the next task to execute from a workflow
func (c *DaemonCoordinator) GetNextTask(ctx context.Context, executionID string) (*config.WorkflowTask, int, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.client.baseURL+"/api/v1/workflow/task/next?execution_id="+executionID, nil)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to create get next task request: %w", err)
	}

	resp, err := c.client.httpClient.Do(req)
	if err != nil {
		return nil, 0, false, fmt.Errorf("get next task request failed: %w", err)
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return nil, 0, false, fmt.Errorf("get next task failed with status %d", resp.StatusCode)
	}

	var taskResp GetNextTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, 0, false, fmt.Errorf("failed to decode get next task response: %w", err)
	}

	if taskResp.Error != "" {
		return nil, 0, false, fmt.Errorf("get next task failed: %s", taskResp.Error)
	}

	return taskResp.Task, taskResp.TaskIndex, taskResp.Complete, nil
}

// CompleteTask marks a task as complete and reports the response
func (c *DaemonCoordinator) CompleteTask(ctx context.Context, executionID string, taskIndex int, response *model.Response) error {
	reqBody := CompleteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   taskIndex,
		Response:    response,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal complete task request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.client.baseURL+"/api/v1/workflow/task/complete", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create complete task request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("complete task request failed: %w", err)
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("complete task failed with status %d", resp.StatusCode)
	}

	var completeResp CompleteTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&completeResp); err != nil {
		return fmt.Errorf("failed to decode complete task response: %w", err)
	}

	if !completeResp.Success {
		return fmt.Errorf("complete task failed: %s", completeResp.Error)
	}

	zap.L().Debug("Completed task via daemon",
		zap.String("execution_id", executionID),
		zap.Int("task_index", taskIndex))

	return nil
}

// GetExecution retrieves workflow execution details from the daemon
func (c *DaemonCoordinator) GetExecution(ctx context.Context, executionID string) (*serving.WorkflowExecution, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.client.baseURL+"/api/v1/workflow/execution/"+executionID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get execution request: %w", err)
	}

	resp, err := c.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get execution request failed: %w", err)
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get execution failed with status %d", resp.StatusCode)
	}

	var execResp GetExecutionResponse
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		return nil, fmt.Errorf("failed to decode get execution response: %w", err)
	}

	if execResp.Error != "" {
		return nil, fmt.Errorf("get execution failed: %s", execResp.Error)
	}

	return execResp.Execution, nil
}
