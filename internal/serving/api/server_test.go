package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/dorcha-inc/orla/internal/serving"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockServingLayer is a test implementation of serving.ServingLayer
type mockServingLayer struct {
	executions map[string]*serving.WorkflowExecution
	lastMaxTokens *int
}

func newMockServingLayer() *mockServingLayer {
	return &mockServingLayer{
		executions: make(map[string]*serving.WorkflowExecution),
	}
}

func (m *mockServingLayer) GetProvider(ctx context.Context, profileName string, task *config.WorkflowTask) (model.Provider, error) {
	return nil, nil
}

func (m *mockServingLayer) StartWorkflow(ctx context.Context, workflowName string) (*serving.WorkflowExecution, error) {
	exec := &serving.WorkflowExecution{
		ExecutionID:      "test-exec-123",
		WorkflowName:     workflowName,
		CurrentTaskIndex: 0,
		Tasks: []*config.WorkflowTask{
			{AgentProfile: "test-profile"},
		},
		State: serving.WorkflowExecutionStatePending,
	}
	m.executions[exec.ExecutionID] = exec
	return exec, nil
}

func (m *mockServingLayer) ExecuteTask(ctx context.Context, execution *serving.WorkflowExecution, taskIndex int, prompt string, maxTokens *int) (*model.Response, error) {
	m.lastMaxTokens = maxTokens
	return &model.Response{
		Content: "test response",
	}, nil
}

func (m *mockServingLayer) GetSharedContext(serverName string) *serving.SharedContext {
	return nil
}

func (m *mockServingLayer) GetExecution(executionID string) (*serving.WorkflowExecution, error) {
	exec, exists := m.executions[executionID]
	if !exists {
		return nil, fmt.Errorf("execution not found")
	}
	return exec, nil
}

func TestServer_HandleExecuteTask_WithMaxTokens(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, _ := json.Marshal(startReq)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err := json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Execute task with max_tokens
	maxTokens := 42
	req := ExecuteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   0,
		Prompt:      "test prompt",
		MaxTokens:   &maxTokens,
	}
	body, _ := json.Marshal(req)
	resp := httptest.NewRecorder()
	httpReq := httptest.NewRequest("POST", "/api/v1/workflow/task/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, httpReq)

	require.Equal(t, http.StatusOK, resp.Code)
	var result ExecuteTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Response)
	assert.Equal(t, "test response", result.Response.Content)
	
	// Verify that maxTokens was passed to the serving layer
	assert.NotNil(t, mockLayer.lastMaxTokens)
	assert.Equal(t, maxTokens, *mockLayer.lastMaxTokens)
}

func TestServer_HandleExecuteTask_WithoutMaxTokens(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, _ := json.Marshal(startReq)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err := json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Execute task without max_tokens
	req := ExecuteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   0,
		Prompt:      "test prompt",
		// MaxTokens is nil (omitted)
	}
	body, _ := json.Marshal(req)
	resp := httptest.NewRecorder()
	httpReq := httptest.NewRequest("POST", "/api/v1/workflow/task/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, httpReq)

	require.Equal(t, http.StatusOK, resp.Code)
	var result ExecuteTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Response)
	
	// Verify that maxTokens was nil when not provided
	assert.Nil(t, mockLayer.lastMaxTokens)
}

func TestServer_HandleExecuteTask_WithMaxTokensZero(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, _ := json.Marshal(startReq)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err := json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Execute task with max_tokens = 0
	maxTokens := 0
	req := ExecuteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   0,
		Prompt:      "test prompt",
		MaxTokens:   &maxTokens,
	}
	body, _ := json.Marshal(req)
	resp := httptest.NewRecorder()
	httpReq := httptest.NewRequest("POST", "/api/v1/workflow/task/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, httpReq)

	require.Equal(t, http.StatusOK, resp.Code)
	var result ExecuteTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)
	
	// Verify that maxTokens was passed even when 0
	assert.NotNil(t, mockLayer.lastMaxTokens)
	assert.Equal(t, 0, *mockLayer.lastMaxTokens)
}
