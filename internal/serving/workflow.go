// Package serving implements the Agentic Serving Layer (RFC 5).
package serving

import (
	"context"
	"fmt"
	"sync"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// WorkflowExecutor executes workflow sequences
type WorkflowExecutor struct {
	// workflows maps workflow names to their configurations
	workflows map[string]*config.Workflow
	// executions tracks active workflow executions
	executions map[string]*WorkflowExecution
	// mu protects access to executions
	mu sync.RWMutex
}

// NewWorkflowExecutor creates a new workflow executor
func NewWorkflowExecutor(workflows []*config.Workflow) *WorkflowExecutor {
	workflowMap := make(map[string]*config.Workflow)
	for _, workflow := range workflows {
		workflowMap[workflow.Name] = workflow
	}

	return &WorkflowExecutor{
		workflows:  workflowMap,
		executions: make(map[string]*WorkflowExecution),
	}
}

// GetWorkflow returns a workflow configuration by name
func (e *WorkflowExecutor) GetWorkflow(name string) (*config.Workflow, error) {
	workflow, exists := e.workflows[name]
	if !exists {
		return nil, fmt.Errorf("workflow '%s' not found", name)
	}
	return workflow, nil
}

// StartWorkflow initializes a workflow execution
func (e *WorkflowExecutor) StartWorkflow(ctx context.Context, workflowName string) (*WorkflowExecution, error) {
	workflow, err := e.GetWorkflow(workflowName)
	if err != nil {
		return nil, err
	}

	// Generate a unique execution ID using UUID
	executionID := uuid.New().String()

	execution := &WorkflowExecution{
		ExecutionID:      executionID,
		WorkflowName:     workflowName,
		CurrentTaskIndex: 0,
		Tasks:            workflow.Tasks,
		State:            WorkflowExecutionStatePending,
		Context:          make([]model.Message, 0),
	}

	e.mu.Lock()
	e.executions[executionID] = execution
	e.mu.Unlock()

	zap.L().Debug("Started workflow execution",
		zap.String("workflow_name", workflowName),
		zap.String("execution_id", executionID),
		zap.Int("task_count", len(workflow.Tasks)))

	return execution, nil
}

// GetExecution returns a workflow execution by ID
func (e *WorkflowExecutor) GetExecution(executionID string) (*WorkflowExecution, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	execution, exists := e.executions[executionID]
	if !exists {
		return nil, fmt.Errorf("workflow execution '%s' not found", executionID)
	}
	return execution, nil
}

// UpdateExecutionState updates the state of a workflow execution
func (e *WorkflowExecutor) UpdateExecutionState(executionID string, state WorkflowExecutionState) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	execution, exists := e.executions[executionID]
	if !exists {
		return fmt.Errorf("workflow execution '%s' not found", executionID)
	}

	execution.State = state
	zap.L().Debug("Updated workflow execution state",
		zap.String("execution_id", executionID),
		zap.String("state", string(state)))

	return nil
}

// AdvanceTask advances to the next task in a workflow execution
func (e *WorkflowExecutor) AdvanceTask(executionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	execution, exists := e.executions[executionID]
	if !exists {
		return fmt.Errorf("workflow execution '%s' not found", executionID)
	}

	if execution.CurrentTaskIndex >= len(execution.Tasks)-1 {
		return fmt.Errorf("workflow execution '%s' has no more tasks", executionID)
	}

	execution.CurrentTaskIndex++
	zap.L().Debug("Advanced workflow execution to next task",
		zap.String("execution_id", executionID),
		zap.Int("task_index", execution.CurrentTaskIndex))

	return nil
}

// IsComplete checks if a workflow execution is complete
func (e *WorkflowExecutor) IsComplete(executionID string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	execution, exists := e.executions[executionID]
	if !exists {
		return false, fmt.Errorf("workflow execution '%s' not found", executionID)
	}

	return execution.CurrentTaskIndex >= len(execution.Tasks)-1, nil
}
