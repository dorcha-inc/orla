package serving

import (
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestWorkflowExecutionState_String(t *testing.T) {
	assert.Equal(t, "pending", string(WorkflowExecutionStatePending))
	assert.Equal(t, "running", string(WorkflowExecutionStateRunning))
	assert.Equal(t, "completed", string(WorkflowExecutionStateCompleted))
	assert.Equal(t, "failed", string(WorkflowExecutionStateFailed))
}

func TestWorkflowExecution_Empty(t *testing.T) {
	exec := &WorkflowExecution{}

	assert.Empty(t, exec.ExecutionID)
	assert.Empty(t, exec.WorkflowName)
	assert.Zero(t, exec.CurrentTaskIndex)
	assert.Empty(t, exec.Tasks)
	assert.Empty(t, exec.State)
	assert.Empty(t, exec.Context)
}

func TestWorkflowExecution_WithValues(t *testing.T) {
	exec := &WorkflowExecution{
		ExecutionID:      "exec-123",
		WorkflowName:     "test-workflow",
		CurrentTaskIndex: 1,
		Tasks:            []*config.WorkflowTask{{AgentProfile: "profile1"}},
		State:            WorkflowExecutionStateRunning,
		Context:          []model.Message{{Role: model.MessageRoleUser, Content: "test"}},
	}

	assert.Equal(t, "exec-123", exec.ExecutionID)
	assert.Equal(t, "test-workflow", exec.WorkflowName)
	assert.Equal(t, 1, exec.CurrentTaskIndex)
	assert.Len(t, exec.Tasks, 1)
	assert.Equal(t, WorkflowExecutionStateRunning, exec.State)
	assert.Len(t, exec.Context, 1)
}
