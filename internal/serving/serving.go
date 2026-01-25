// Package serving implements the Agentic Serving Layer (RFC 5).
package serving

import (
	"context"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
)

// ServingLayer is the core interface for the Agentic Serving Layer
type ServingLayer interface {
	// GetProvider returns a model provider for an agent profile or workflow task
	// If task is nil, uses the agent profile's LLM server configuration
	// If task is not nil and has an LLM server override, uses that instead
	GetProvider(ctx context.Context, profileName string, task *config.WorkflowTask) (model.Provider, error)

	// StartWorkflow initializes a workflow execution
	StartWorkflow(ctx context.Context, workflowName string) (*WorkflowExecution, error)

	// ExecuteTask executes a single workflow task with the given prompt
	// Returns the response from the task execution
	ExecuteTask(ctx context.Context, execution *WorkflowExecution, taskIndex int, prompt string, maxTokens *int) (*model.Response, error)

	// GetSharedContext returns shared context for a given LLM server
	GetSharedContext(serverName string) *SharedContext

	// GetExecution returns a workflow execution by ID
	GetExecution(executionID string) (*WorkflowExecution, error)
}

// WorkflowExecution represents an active workflow execution
type WorkflowExecution struct {
	// ExecutionID is the unique identifier for this execution
	ExecutionID string
	// WorkflowName is the name of the workflow being executed
	WorkflowName string
	// CurrentTaskIndex is the index of the current task (0-based)
	CurrentTaskIndex int
	// Tasks is the list of tasks in the workflow
	Tasks []*config.WorkflowTask
	// State is the execution state
	State WorkflowExecutionState
	// Context holds the accumulated messages from previous tasks
	// This is used when tasks have UseContext: true
	Context []model.Message
}

// WorkflowExecutionState represents the state of a workflow execution
type WorkflowExecutionState string

const (
	// WorkflowExecutionStatePending indicates the workflow is pending execution
	WorkflowExecutionStatePending WorkflowExecutionState = "pending"
	// WorkflowExecutionStateRunning indicates the workflow is currently running
	WorkflowExecutionStateRunning WorkflowExecutionState = "running"
	// WorkflowExecutionStateCompleted indicates the workflow has completed
	WorkflowExecutionStateCompleted WorkflowExecutionState = "completed"
	// WorkflowExecutionStateFailed indicates the workflow has failed
	WorkflowExecutionStateFailed WorkflowExecutionState = "failed"
)
