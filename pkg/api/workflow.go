package orla

import (
	"context"
	"fmt"
)

// WorkflowExecutor provides high-level workflow execution with automatic task orchestration.
// It uses the daemon's /api/v1/workflow/task/execute endpoint for inference (remote execution mode).
// The daemon resolves server names from agent profiles, so no config is needed.
type WorkflowExecutor struct {
	client *Client
}

// NewWorkflowExecutor creates a new workflow executor that uses remote execution
// (daemon handles inference via /api/v1/workflow/task/execute)
// The daemon reads orla.yaml and resolves server names from agent profiles automatically.
func NewWorkflowExecutor(daemonURL string) *WorkflowExecutor {
	return &WorkflowExecutor{
		client: NewClient(daemonURL),
	}
}

// ExecuteWorkflow executes a complete workflow, handling all task orchestration
// Returns a slice of task responses in order
func (e *WorkflowExecutor) ExecuteWorkflow(ctx context.Context, workflowName string, initialPrompt string, maxTokensPerTask int) ([]*TaskResponse, error) {
	// Start workflow
	executionID, err := e.client.StartWorkflow(ctx, workflowName)
	if err != nil {
		return nil, fmt.Errorf("failed to start workflow: %w", err)
	}

	var responses []*TaskResponse
	currentPrompt := initialPrompt

	for {
		// Get next task (daemon resolves server name from agent profile)
		task, taskIndex, complete, serverName, err := e.client.GetNextTask(ctx, executionID)
		if err != nil {
			return nil, fmt.Errorf("failed to get next task: %w", err)
		}

		if complete {
			break
		}

		// Determine prompt for this task
		taskPrompt := currentPrompt
		if task.Prompt != "" {
			taskPrompt = task.Prompt
		}

		// Get shared context if needed (server name is resolved by daemon)
		if task.UseContext && serverName != "" {

			messages, err := e.client.GetContext(ctx, serverName)
			if err != nil {
				// Log but continue - context retrieval is best-effort
				fmt.Printf("Warning: failed to get context for server %s: %v\n", serverName, err)
			} else {
				// Prepend context to prompt
				var contextStr string
				for _, msg := range messages {
					contextStr += fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content)
				}
				taskPrompt = contextStr + "\n" + taskPrompt
			}
		}

		// Execute task (daemon handles inference)
		response, err := e.client.ExecuteTask(ctx, executionID, taskIndex, taskPrompt, maxTokensPerTask)
		if err != nil {
			return nil, fmt.Errorf("failed to execute task %d: %w", taskIndex, err)
		}

		responses = append(responses, response)

		// Complete task
		if err := e.client.CompleteTask(ctx, executionID, taskIndex, response); err != nil {
			return nil, fmt.Errorf("failed to complete task: %w", err)
		}

		// Sync context if needed
		// If task.UseContext is true, we sync to the server's shared context
		// The daemon will handle whether that server actually has shared context enabled
		if response.Content != "" && task.UseContext && serverName != "" {
			syncMessages := []Message{
				{Role: "user", Content: taskPrompt},
				{Role: "assistant", Content: response.Content},
			}
			if err := e.client.SyncContext(ctx, serverName, syncMessages); err != nil {
				// Log but don't fail - context sync is best-effort
				// The daemon will reject if the server doesn't have shared context enabled
				fmt.Printf("Warning: failed to sync context: %v\n", err)
			}
		}

		// Update current prompt for next task
		if response.Content != "" {
			currentPrompt = response.Content
		}
	}

	return responses, nil
}

// ExecuteWorkflowWithCallback executes a workflow and calls a callback for each task
// This allows custom handling of task responses
func (e *WorkflowExecutor) ExecuteWorkflowWithCallback(
	ctx context.Context,
	workflowName string,
	initialPrompt string,
	maxTokensPerTask int,
	onTask func(taskIndex int, task *WorkflowTask, response *TaskResponse) error,
) error {
	executionID, err := e.client.StartWorkflow(ctx, workflowName)
	if err != nil {
		return fmt.Errorf("failed to start workflow: %w", err)
	}

	currentPrompt := initialPrompt

	for {
		// Get next task (daemon resolves server name from agent profile)
		task, taskIndex, complete, serverName, err := e.client.GetNextTask(ctx, executionID)
		if err != nil {
			return fmt.Errorf("failed to get next task: %w", err)
		}

		if complete {
			break
		}

		taskPrompt := currentPrompt
		if task.Prompt != "" {
			taskPrompt = task.Prompt
		}

		// Get context if needed (server name is resolved by daemon)
		if task.UseContext && serverName != "" {
			messages, err := e.client.GetContext(ctx, serverName)
			if err != nil {
				// Log but continue
				fmt.Printf("Warning: failed to get context: %v\n", err)
			} else {
				var contextStr string
				for _, msg := range messages {
					contextStr += fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content)
				}
				taskPrompt = contextStr + "\n" + taskPrompt
			}
		}

		// Execute
		response, err := e.client.ExecuteTask(ctx, executionID, taskIndex, taskPrompt, maxTokensPerTask)
		if err != nil {
			return fmt.Errorf("failed to execute task %d: %w", taskIndex, err)
		}

		// Call callback
		if onTask != nil {
			if err := onTask(taskIndex, task, response); err != nil {
				return fmt.Errorf("callback error for task %d: %w", taskIndex, err)
			}
		}

		// Complete
		if err := e.client.CompleteTask(ctx, executionID, taskIndex, response); err != nil {
			return fmt.Errorf("failed to complete task: %w", err)
		}

		// Sync context if needed
		// The daemon will handle whether the server has shared context enabled
		if response.Content != "" && task.UseContext && serverName != "" {
			syncMessages := []Message{
				{Role: "user", Content: taskPrompt},
				{Role: "assistant", Content: response.Content},
			}
			err := e.client.SyncContext(ctx, serverName, syncMessages)
			if err != nil {
				return fmt.Errorf("failed to sync context: %w", err)
			}
		}

		if response.Content != "" {
			currentPrompt = response.Content
		}
	}

	return nil
}
