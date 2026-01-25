package orla

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkflowExecutor(t *testing.T) {
	executor := NewWorkflowExecutor("http://localhost:8081")
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.client)
	assert.Equal(t, "http://localhost:8081", executor.client.baseURL)
}

const (
	startWorkflowPath = "/api/v1/workflow/start"
	nextTaskPath      = "/api/v1/workflow/task/next"
	executeTaskPath   = "/api/v1/workflow/task/execute"
	completeTaskPath  = "/api/v1/workflow/task/complete"
	contextPath       = "/api/v1/context"
)

func TestWorkflowExecutor_ExecuteWorkflow_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			var req StartWorkflowRequest
			_ = decodeJSON(r, &req) //nolint:errcheck
			assert.Equal(t, "test-workflow", req.WorkflowName)

			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			if callCount == 2 {
				// First task
				response := GetNextTaskResponse{
					Task: &WorkflowTask{
						AgentProfile: "test-profile",
						Prompt:       "",
						UseContext:   false,
					},
					TaskIndex: 0,
					Complete:  false,
				}
				encodeJSON(w, response)
			} else {
				// Complete
				response := GetNextTaskResponse{
					Complete: true,
				}
				encodeJSON(w, response)
			}

		case executeTaskPath:
			var req ExecuteTaskRequest
			_ = decodeJSON(r, &req) //nolint:errcheck
			assert.Equal(t, "exec-123", req.ExecutionID)
			assert.Equal(t, 0, req.TaskIndex)

			response := ExecuteTaskResponse{
				Success: true,
				Response: &TaskResponse{
					Content: "task response",
				},
			}
			encodeJSON(w, response)

		case completeTaskPath:
			var req CompleteTaskRequest
			_ = decodeJSON(r, &req) //nolint:errcheck
			assert.Equal(t, "exec-123", req.ExecutionID)
			assert.Equal(t, 0, req.TaskIndex)

			response := CompleteTaskResponse{
				Success: true,
			}
			encodeJSON(w, response)
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()
	responses, err := executor.ExecuteWorkflow(ctx, "test-workflow", "initial prompt", 100)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
	assert.Equal(t, "task response", responses[0].Content)
}

func TestWorkflowExecutor_ExecuteWorkflow_WithContext(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			if callCount == 2 {
				response := GetNextTaskResponse{
					Task: &WorkflowTask{
						AgentProfile: "test-profile",
						UseContext:   true,
					},
					TaskIndex: 0,
					Complete:  false,
					LLMServer: "test-server",
				}
				encodeJSON(w, response)
			} else {
				response := GetNextTaskResponse{
					Complete: true,
				}
				encodeJSON(w, response)
			}

		case contextPath + "/test-server":
			response := GetContextResponse{
				Messages: []Message{
					{Role: "user", Content: "context message"},
				},
			}
			encodeJSON(w, response)

		case executeTaskPath:
			response := ExecuteTaskResponse{
				Success: true,
				Response: &TaskResponse{
					Content: "task response",
				},
			}
			encodeJSON(w, response)

		case completeTaskPath:
			response := CompleteTaskResponse{
				Success: true,
			}
			encodeJSON(w, response)

		case contextPath + "/sync":
			var req SyncContextRequest
			_ = decodeJSON(r, &req) //nolint:errcheck
			assert.Equal(t, "test-server", req.ServerName)
			assert.Len(t, req.Messages, 2)

			response := SyncContextResponse{
				Success: true,
			}
			encodeJSON(w, response)
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()
	responses, err := executor.ExecuteWorkflow(ctx, "test-workflow", "initial prompt", 100)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
}

func TestWorkflowExecutor_ExecuteWorkflow_WithTaskPrompt(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			if callCount == 2 {
				response := GetNextTaskResponse{
					Task: &WorkflowTask{
						AgentProfile: "test-profile",
						Prompt:       "task-specific prompt",
						UseContext:   false,
					},
					TaskIndex: 0,
					Complete:  false,
				}
				encodeJSON(w, response)
			} else {
				response := GetNextTaskResponse{
					Complete: true,
				}
				encodeJSON(w, response)
			}

		case executeTaskPath:
			var req ExecuteTaskRequest
			_ = decodeJSON(r, &req) //nolint:errcheck
			// Should use task prompt, not initial prompt
			assert.Equal(t, "task-specific prompt", req.Prompt)

			response := ExecuteTaskResponse{
				Success: true,
				Response: &TaskResponse{
					Content: "task response",
				},
			}
			encodeJSON(w, response)

		case completeTaskPath:
			response := CompleteTaskResponse{
				Success: true,
			}
			encodeJSON(w, response)
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()
	responses, err := executor.ExecuteWorkflow(ctx, "test-workflow", "initial prompt", 100)
	require.NoError(t, err)
	assert.Len(t, responses, 1)
}

func TestWorkflowExecutor_ExecuteWorkflow_StartWorkflowError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request")) //nolint:errcheck
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()
	_, err := executor.ExecuteWorkflow(ctx, "test-workflow", "initial prompt", 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start workflow")
}

func TestWorkflowExecutor_ExecuteWorkflow_GetNextTaskError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("bad request")) //nolint:errcheck
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()
	_, err := executor.ExecuteWorkflow(ctx, "test-workflow", "initial prompt", 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get next task")
}

func TestWorkflowExecutor_ExecuteWorkflowWithCallback_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			if callCount == 2 {
				response := GetNextTaskResponse{
					Task: &WorkflowTask{
						AgentProfile: "test-profile",
						UseContext:   false,
					},
					TaskIndex: 0,
					Complete:  false,
				}
				encodeJSON(w, response)
			} else {
				response := GetNextTaskResponse{
					Complete: true,
				}
				encodeJSON(w, response)
			}

		case executeTaskPath:
			response := ExecuteTaskResponse{
				Success: true,
				Response: &TaskResponse{
					Content: "task response",
				},
			}
			encodeJSON(w, response)

		case completeTaskPath:
			response := CompleteTaskResponse{
				Success: true,
			}
			encodeJSON(w, response)
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()

	callbackCalled := false
	callback := func(taskIndex int, task *WorkflowTask, response *TaskResponse) error {
		callbackCalled = true
		assert.Equal(t, 0, taskIndex)
		assert.NotNil(t, task)
		assert.NotNil(t, response)
		return nil
	}

	err := executor.ExecuteWorkflowWithCallback(ctx, "test-workflow", "initial prompt", 100, callback)
	require.NoError(t, err)
	assert.True(t, callbackCalled)
}

func TestWorkflowExecutor_ExecuteWorkflowWithCallback_CallbackError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			if callCount == 2 {
				response := GetNextTaskResponse{
					Task: &WorkflowTask{
						AgentProfile: "test-profile",
					},
					TaskIndex: 0,
					Complete:  false,
				}
				encodeJSON(w, response)
			} else {
				response := GetNextTaskResponse{
					Complete: true,
				}
				encodeJSON(w, response)
			}

		case executeTaskPath:
			response := ExecuteTaskResponse{
				Success: true,
				Response: &TaskResponse{
					Content: "task response",
				},
			}
			encodeJSON(w, response)

		case completeTaskPath:
			response := CompleteTaskResponse{
				Success: true,
			}
			encodeJSON(w, response)
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()

	callback := func(taskIndex int, task *WorkflowTask, response *TaskResponse) error {
		return assert.AnError
	}

	err := executor.ExecuteWorkflowWithCallback(ctx, "test-workflow", "initial prompt", 100, callback)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "callback error")
}

func TestWorkflowExecutor_ExecuteWorkflowWithCallback_ContextSyncError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case startWorkflowPath:
			response := StartWorkflowResponse{
				ExecutionID: "exec-123",
			}
			encodeJSON(w, response)

		case nextTaskPath:
			if callCount == 2 {
				response := GetNextTaskResponse{
					Task: &WorkflowTask{
						AgentProfile: "test-profile",
						UseContext:   true,
					},
					TaskIndex: 0,
					Complete:  false,
					LLMServer: "test-server",
				}
				encodeJSON(w, response)
			} else {
				response := GetNextTaskResponse{
					Complete: true,
				}
				encodeJSON(w, response)
			}

		case contextPath + "/test-server":
			response := GetContextResponse{
				Messages: []Message{},
			}
			encodeJSON(w, response)

		case executeTaskPath:
			response := ExecuteTaskResponse{
				Success: true,
				Response: &TaskResponse{
					Content: "task response",
				},
			}
			encodeJSON(w, response)

		case completeTaskPath:
			response := CompleteTaskResponse{
				Success: true,
			}
			encodeJSON(w, response)

		case contextPath + "/sync":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("sync failed")) //nolint:errcheck
		}
	}))
	defer server.Close()

	executor := NewWorkflowExecutor(server.URL)
	ctx := context.Background()

	err := executor.ExecuteWorkflowWithCallback(ctx, "test-workflow", "initial prompt", 100, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to sync context")
}
