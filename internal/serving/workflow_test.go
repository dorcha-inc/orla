package serving

import (
	"context"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkflowExecutor(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "workflow1",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
			},
		},
		{
			Name: "workflow2",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile2"},
			},
		},
	}

	executor := NewWorkflowExecutor(workflows)
	require.NotNil(t, executor)
	assert.NotNil(t, executor.workflows)
	assert.NotNil(t, executor.executions)
	
	workflow, err := executor.GetWorkflow("workflow1")
	require.NoError(t, err)
	assert.Equal(t, "workflow1", workflow.Name)
	
	workflow, err = executor.GetWorkflow("workflow2")
	require.NoError(t, err)
	assert.Equal(t, "workflow2", workflow.Name)
}

func TestWorkflowExecutor_GetWorkflow_NotFound(t *testing.T) {
	executor := NewWorkflowExecutor([]*config.Workflow{})
	
	workflow, err := executor.GetWorkflow("nonexistent")
	assert.Error(t, err)
	assert.Nil(t, workflow)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowExecutor_StartWorkflow(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
				{AgentProfile: "profile2"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	require.NotNil(t, execution)
	
	assert.NotEmpty(t, execution.ExecutionID)
	assert.Equal(t, "test-workflow", execution.WorkflowName)
	assert.Equal(t, 0, execution.CurrentTaskIndex)
	assert.Equal(t, WorkflowExecutionStatePending, execution.State)
	assert.Len(t, execution.Tasks, 2)
	assert.NotNil(t, execution.Context)
	assert.Len(t, execution.Context, 0)
	
	// Should be able to retrieve the execution
	retrieved, err := executor.GetExecution(execution.ExecutionID)
	require.NoError(t, err)
	assert.Equal(t, execution, retrieved)
}

func TestWorkflowExecutor_StartWorkflow_NotFound(t *testing.T) {
	executor := NewWorkflowExecutor([]*config.Workflow{})
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "nonexistent")
	assert.Error(t, err)
	assert.Nil(t, execution)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowExecutor_StartWorkflow_UniqueExecutionIDs(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution1, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	execution2, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	// Execution IDs should be unique
	assert.NotEqual(t, execution1.ExecutionID, execution2.ExecutionID)
}

func TestWorkflowExecutor_GetExecution_NotFound(t *testing.T) {
	executor := NewWorkflowExecutor([]*config.Workflow{})
	
	execution, err := executor.GetExecution("nonexistent-id")
	assert.Error(t, err)
	assert.Nil(t, execution)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowExecutor_UpdateExecutionState(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	// Update state to running
	err = executor.UpdateExecutionState(execution.ExecutionID, WorkflowExecutionStateRunning)
	require.NoError(t, err)
	
	// Verify state was updated
	retrieved, err := executor.GetExecution(execution.ExecutionID)
	require.NoError(t, err)
	assert.Equal(t, WorkflowExecutionStateRunning, retrieved.State)
	
	// Update to completed
	err = executor.UpdateExecutionState(execution.ExecutionID, WorkflowExecutionStateCompleted)
	require.NoError(t, err)
	
	retrieved, err = executor.GetExecution(execution.ExecutionID)
	require.NoError(t, err)
	assert.Equal(t, WorkflowExecutionStateCompleted, retrieved.State)
}

func TestWorkflowExecutor_UpdateExecutionState_NotFound(t *testing.T) {
	executor := NewWorkflowExecutor([]*config.Workflow{})
	
	err := executor.UpdateExecutionState("nonexistent-id", WorkflowExecutionStateRunning)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowExecutor_AdvanceTask(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
				{AgentProfile: "profile2"},
				{AgentProfile: "profile3"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	assert.Equal(t, 0, execution.CurrentTaskIndex)
	
	// Advance to next task
	err = executor.AdvanceTask(execution.ExecutionID)
	require.NoError(t, err)
	
	retrieved, err := executor.GetExecution(execution.ExecutionID)
	require.NoError(t, err)
	assert.Equal(t, 1, retrieved.CurrentTaskIndex)
	
	// Advance again
	err = executor.AdvanceTask(execution.ExecutionID)
	require.NoError(t, err)
	
	retrieved, err = executor.GetExecution(execution.ExecutionID)
	require.NoError(t, err)
	assert.Equal(t, 2, retrieved.CurrentTaskIndex)
}

func TestWorkflowExecutor_AdvanceTask_NotFound(t *testing.T) {
	executor := NewWorkflowExecutor([]*config.Workflow{})
	
	err := executor.AdvanceTask("nonexistent-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowExecutor_AdvanceTask_NoMoreTasks(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	// Try to advance when already at last task
	err = executor.AdvanceTask(execution.ExecutionID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no more tasks")
}

func TestWorkflowExecutor_IsComplete(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
				{AgentProfile: "profile2"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	// Initially not complete (at task 0, need to reach task 1)
	isComplete, err := executor.IsComplete(execution.ExecutionID)
	require.NoError(t, err)
	assert.False(t, isComplete)
	
	// Advance to last task
	err = executor.AdvanceTask(execution.ExecutionID)
	require.NoError(t, err)
	
	// Now should be complete (at task 1, which is last)
	isComplete, err = executor.IsComplete(execution.ExecutionID)
	require.NoError(t, err)
	assert.True(t, isComplete)
}

func TestWorkflowExecutor_IsComplete_SingleTask(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	// Single task workflow should be complete immediately (at task 0, which is last)
	isComplete, err := executor.IsComplete(execution.ExecutionID)
	require.NoError(t, err)
	assert.True(t, isComplete)
}

func TestWorkflowExecutor_IsComplete_NotFound(t *testing.T) {
	executor := NewWorkflowExecutor([]*config.Workflow{})
	
	isComplete, err := executor.IsComplete("nonexistent-id")
	assert.Error(t, err)
	assert.False(t, isComplete)
	assert.Contains(t, err.Error(), "not found")
}

func TestWorkflowExecutor_ConcurrentAccess(t *testing.T) {
	workflows := []*config.Workflow{
		{
			Name: "test-workflow",
			Tasks: []*config.WorkflowTask{
				{AgentProfile: "profile1"},
			},
		},
	}
	executor := NewWorkflowExecutor(workflows)
	
	ctx := context.Background()
	execution, err := executor.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	
	// Test concurrent state updates
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			err := executor.UpdateExecutionState(execution.ExecutionID, WorkflowExecutionStateRunning)
			require.NoError(t, err)
			done <- true
		}()
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Should still be able to retrieve execution
	retrieved, err := executor.GetExecution(execution.ExecutionID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}
