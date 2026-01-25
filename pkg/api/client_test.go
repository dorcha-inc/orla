package orla

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to encode JSON response in test handlers
// Errors are ignored in test handlers as they indicate test setup issues
func encodeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck // Test handler - errors indicate test setup issues
}

// Helper function to decode JSON request in test handlers
// Returns error for cases where we need to check it
func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func TestNewClient(t *testing.T) {
	client := NewClient("http://localhost:8081")
	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8081", client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
}

func TestClient_Health_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/health", r.URL.Path)
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	err := client.Health(ctx)
	assert.NoError(t, err)
}

func TestClient_Health_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	err := client.Health(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestClient_Health_RequestError(t *testing.T) {
	// Create a server that closes immediately to simulate connection error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close() // Close immediately to cause connection error

	client := NewClient(server.URL)
	ctx := context.Background()
	err := client.Health(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check health")
}

func TestClient_StartWorkflow_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workflow/start", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req StartWorkflowRequest
		_ = decodeJSON(r, &req) //nolint:errcheck
		assert.Equal(t, "test-workflow", req.WorkflowName)

		response := StartWorkflowResponse{
			ExecutionID: "exec-123",
		}
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	execID, err := client.StartWorkflow(ctx, "test-workflow")
	require.NoError(t, err)
	assert.Equal(t, "exec-123", execID)
}

func TestClient_StartWorkflow_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := StartWorkflowResponse{
			Error: "workflow not found",
		}
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	_, err := client.StartWorkflow(ctx, "test-workflow")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not found")
}

func TestClient_StartWorkflow_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request")) //nolint:errcheck
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	_, err := client.StartWorkflow(ctx, "test-workflow")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}

func TestClient_GetNextTask_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workflow/task/next", r.URL.Path)
		assert.Equal(t, "exec-123", r.URL.Query().Get("execution_id"))

		response := GetNextTaskResponse{
			Task: &WorkflowTask{
				AgentProfile: "test-profile",
				Prompt:       "test prompt",
				UseContext:   true,
			},
			TaskIndex: 0,
			Complete:  false,
			LLMServer: "test-server",
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	task, taskIndex, complete, serverName, err := client.GetNextTask(ctx, "exec-123")
	require.NoError(t, err)
	assert.NotNil(t, task)
	assert.Equal(t, "test-profile", task.AgentProfile)
	assert.Equal(t, 0, taskIndex)
	assert.False(t, complete)
	assert.Equal(t, "test-server", serverName)
}

func TestClient_GetNextTask_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GetNextTaskResponse{
			Complete: true,
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	_, _, complete, _, err := client.GetNextTask(ctx, "exec-123")
	require.NoError(t, err)
	assert.True(t, complete)
}

func TestClient_ExecuteTask_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workflow/task/execute", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req ExecuteTaskRequest
		require.NoError(t, decodeJSON(r, &req))
		assert.Equal(t, "exec-123", req.ExecutionID)
		assert.Equal(t, 0, req.TaskIndex)
		assert.Equal(t, "test prompt", req.Prompt)
		assert.NotNil(t, req.MaxTokens)
		assert.Equal(t, 100, *req.MaxTokens)

		response := ExecuteTaskResponse{
			Success: true,
			Response: &TaskResponse{
				Content:  "test response",
				Thinking: "test thinking",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	taskResp, err := client.ExecuteTask(ctx, "exec-123", 0, "test prompt", 100)
	require.NoError(t, err)
	assert.NotNil(t, taskResp)
	assert.Equal(t, "test response", taskResp.Content)
	assert.Equal(t, "test thinking", taskResp.Thinking)
}

func TestClient_ExecuteTask_WithMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ExecuteTaskRequest
		_ = decodeJSON(r, &req) //nolint:errcheck
		assert.NotNil(t, req.MaxTokens)
		assert.Equal(t, 50, *req.MaxTokens)

		response := ExecuteTaskResponse{
			Success: true,
			Response: &TaskResponse{
				Content: "test response",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	taskResp, err := client.ExecuteTask(ctx, "exec-123", 0, "test prompt", 50)
	require.NoError(t, err)
	assert.NotNil(t, taskResp)
}

func TestClient_ExecuteTask_WithoutMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ExecuteTaskRequest
		_ = decodeJSON(r, &req) //nolint:errcheck
		assert.Nil(t, req.MaxTokens) // Should be nil when maxTokens <= 0

		response := ExecuteTaskResponse{
			Success: true,
			Response: &TaskResponse{
				Content: "test response",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	taskResp, err := client.ExecuteTask(ctx, "exec-123", 0, "test prompt", 0)
	require.NoError(t, err)
	assert.NotNil(t, taskResp)
}

func TestClient_ExecuteTask_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ExecuteTaskResponse{
			Success: false,
			Error:   "execution failed",
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	_, err := client.ExecuteTask(ctx, "exec-123", 0, "test prompt", 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execution failed")
}

func TestClient_CompleteTask_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workflow/task/complete", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req CompleteTaskRequest
		require.NoError(t, decodeJSON(r, &req))
		assert.Equal(t, "exec-123", req.ExecutionID)
		assert.Equal(t, 0, req.TaskIndex)
		assert.NotNil(t, req.Response)

		response := CompleteTaskResponse{
			Success: true,
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	taskResp := &TaskResponse{Content: "test response"}
	err := client.CompleteTask(ctx, "exec-123", 0, taskResp)
	assert.NoError(t, err)
}

func TestClient_CompleteTask_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := CompleteTaskResponse{
			Success: false,
			Error:   "completion failed",
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	taskResp := &TaskResponse{Content: "test response"}
	err := client.CompleteTask(ctx, "exec-123", 0, taskResp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "completion failed")
}

func TestClient_GetContext_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/context/test-server", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		response := GetContextResponse{
			Messages: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	messages, err := client.GetContext(ctx, "test-server")
	require.NoError(t, err)
	assert.Len(t, messages, 2)
	assert.Equal(t, "user", messages[0].Role)
	assert.Equal(t, "hello", messages[0].Content)
}

func TestClient_GetContext_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GetContextResponse{
			Error: "context not found",
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	_, err := client.GetContext(ctx, "test-server")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context not found")
}

func TestClient_SyncContext_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/context/sync", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req SyncContextRequest
		require.NoError(t, decodeJSON(r, &req))
		assert.Equal(t, "test-server", req.ServerName)
		assert.Len(t, req.Messages, 2)

		response := SyncContextResponse{
			Success: true,
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	err := client.SyncContext(ctx, "test-server", messages)
	assert.NoError(t, err)
}

func TestClient_SyncContext_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := SyncContextResponse{
			Success: false,
			Error:   "sync failed",
		}
		w.Header().Set("Content-Type", "application/json")
		encodeJSON(w, response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	messages := []Message{
		{Role: "user", Content: "hello"},
	}
	err := client.SyncContext(ctx, "test-server", messages)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync failed")
}
