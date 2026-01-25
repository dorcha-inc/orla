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
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
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
	body, err := json.Marshal(req)
	require.NoError(t, err)
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
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Execute task without max_tokens
	req := ExecuteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   0,
		Prompt:      "test prompt",
		// MaxTokens is nil (omitted)
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)
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
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
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
	body, err := json.Marshal(req)
	require.NoError(t, err)
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

func TestServer_HandleHealth(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result map[string]string
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "healthy", result["status"])
}

func TestServer_HandleContextSync_Success(t *testing.T) {
	mockCtx := serving.NewSharedContext("test-server", 100)
	mockLayer := &mockServingLayerWithContext{
		mockServingLayer: *newMockServingLayer(),
		sharedContext:    mockCtx,
	}
	server := NewServer(mockLayer, ":0")

	reqBody := ContextSyncRequest{
		ServerName: "test-server",
		Messages: []model.Message{
			{Role: model.MessageRoleUser, Content: "test message"},
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/context/sync", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result ContextSyncResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 1, len(mockCtx.GetMessages()))
}

func TestServer_HandleContextSync_NotFound(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	reqBody := ContextSyncRequest{
		ServerName: "nonexistent-server",
		Messages:   []model.Message{},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/context/sync", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusNotFound, resp.Code)
	var result ContextSyncResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "no shared context")
}

func TestServer_HandleContextSync_InvalidJSON(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/context/sync", bytes.NewReader([]byte("invalid json")))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleGetContext_Success(t *testing.T) {
	mockCtx := serving.NewSharedContext("test-server", 100)
	mockCtx.AppendMessage(model.Message{Role: model.MessageRoleUser, Content: "test message 1"})
	mockCtx.AppendMessage(model.Message{Role: model.MessageRoleAssistant, Content: "test response"})
	mockLayer := &mockServingLayerWithContext{
		mockServingLayer: *newMockServingLayer(),
		sharedContext:    mockCtx,
	}
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/context/test-server", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result GetContextResponse
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Messages))
	assert.Equal(t, "test message 1", result.Messages[0].Content)
}

func TestServer_HandleGetContext_NotFound(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/context/nonexistent-server", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusNotFound, resp.Code)
	var result GetContextResponse
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "no shared context")
}

func TestServer_HandleGetContext_NoServerName(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/context/", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleGetNextTask_Success(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Get next task
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/workflow/task/next?execution_id=%s", executionID), nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result GetNextTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.NotNil(t, result.Task)
	assert.Equal(t, 0, result.TaskIndex)
	assert.False(t, result.Complete)
}

func TestServer_HandleGetNextTask_NoExecutionID(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/workflow/task/next", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleGetNextTask_NotFound(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/workflow/task/next?execution_id=nonexistent", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusNotFound, resp.Code)
	var result GetNextTaskResponse
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "execution not found")
}

func TestServer_HandleGetNextTask_Complete(t *testing.T) {
	mockLayer := newMockServingLayer()
	exec := &serving.WorkflowExecution{
		ExecutionID:      "complete-exec",
		CurrentTaskIndex: 1,
		Tasks:           []*config.WorkflowTask{{AgentProfile: "test-profile"}},
		State:           serving.WorkflowExecutionStatePending,
	}
	mockLayer.executions["complete-exec"] = exec

	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/workflow/task/next?execution_id=complete-exec", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result GetNextTaskResponse
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Complete)
}

func TestServer_HandleCompleteTask_Success(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Complete task
	completeReq := CompleteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   0,
		Response:    &model.Response{Content: "task completed"},
	}
	body, err := json.Marshal(completeReq)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/workflow/task/complete", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result CompleteTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)

	// Verify execution was updated
	exec, err := mockLayer.GetExecution(executionID)
	require.NoError(t, err)
	assert.Equal(t, 1, exec.CurrentTaskIndex)
}

func TestServer_HandleCompleteTask_NotFound(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	completeReq := CompleteTaskRequest{
		ExecutionID: "nonexistent",
		TaskIndex:   0,
	}
	body, err := json.Marshal(completeReq)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/workflow/task/complete", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusNotFound, resp.Code)
	var result CompleteTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.False(t, result.Success)
}

func TestServer_HandleCompleteTask_IndexMismatch(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Try to complete with wrong index
	completeReq := CompleteTaskRequest{
		ExecutionID: executionID,
		TaskIndex:   5, // Wrong index
	}
	body, err := json.Marshal(completeReq)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/workflow/task/complete", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	var result CompleteTaskResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "task index mismatch")
}

func TestServer_HandleGetExecution_Success(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	// Start a workflow first
	startReq := WorkflowStartRequest{WorkflowName: "test-workflow"}
	startBody, err := json.Marshal(startReq)
	require.NoError(t, err)
	startResp := httptest.NewRecorder()
	startHTTPReq := httptest.NewRequest("POST", "/api/v1/workflow/start", bytes.NewReader(startBody))
	server.mux.ServeHTTP(startResp, startHTTPReq)
	require.Equal(t, http.StatusOK, startResp.Code)

	var startResult WorkflowStartResponse
	err = json.Unmarshal(startResp.Body.Bytes(), &startResult)
	require.NoError(t, err)
	executionID := startResult.ExecutionID

	// Get execution
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/workflow/execution/%s", executionID), nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result GetExecutionResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.NotNil(t, result.Execution)
	assert.Equal(t, executionID, result.Execution.ExecutionID)
}

func TestServer_HandleGetExecution_NotFound(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/workflow/execution/nonexistent", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusNotFound, resp.Code)
	var result GetExecutionResponse
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "execution not found")
}

func TestServer_HandleGetExecution_NoExecutionID(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/workflow/execution/", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleAgentExecute_Success(t *testing.T) {
	mockProvider := &mockProviderForAPI{
		chatResponse: &model.Response{Content: "agent response"},
	}
	mockLayer := &mockServingLayerWithProvider{
		mockServingLayer: *newMockServingLayer(),
		provider:         mockProvider,
	}
	server := NewServer(mockLayer, ":0")

	reqBody := AgentExecuteRequest{
		ProfileName: "test-profile",
		Prompt:      "test prompt",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/agent/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result AgentExecuteResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Response)
	assert.Equal(t, "agent response", result.Response.Content)
}

func TestServer_HandleAgentExecute_NoProfileName(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	reqBody := AgentExecuteRequest{
		Prompt: "test prompt",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/agent/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleAgentExecute_InvalidJSON(t *testing.T) {
	mockLayer := newMockServingLayer()
	server := NewServer(mockLayer, ":0")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/agent/execute", bytes.NewReader([]byte("invalid json")))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

// mockServingLayerWithContext extends mockServingLayer with SharedContext support
type mockServingLayerWithContext struct {
	mockServingLayer
	sharedContext *serving.SharedContext
}

func (m *mockServingLayerWithContext) GetSharedContext(serverName string) *serving.SharedContext {
	if serverName == "test-server" && m.sharedContext != nil {
		return m.sharedContext
	}
	return nil
}

// mockProviderForAPI is a test implementation of model.Provider
type mockProviderForAPI struct {
	chatResponse *model.Response
	chatError    error
}

func (m *mockProviderForAPI) Name() string {
	return "mock-provider"
}

func (m *mockProviderForAPI) Chat(ctx context.Context, messages []model.Message, tools []*mcp.Tool, stream bool, maxTokens *int) (*model.Response, <-chan model.StreamEvent, error) {
	if m.chatError != nil {
		return nil, nil, m.chatError
	}
	return m.chatResponse, nil, nil
}

func (m *mockProviderForAPI) EnsureReady(ctx context.Context) error {
	return nil
}

// mockServingLayerWithProvider extends mockServingLayer with provider support
type mockServingLayerWithProvider struct {
	mockServingLayer
	provider model.Provider
}

func (m *mockServingLayerWithProvider) GetProvider(ctx context.Context, profileName string, task *config.WorkflowTask) (model.Provider, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("provider not found")
	}
	return m.provider, nil
}
