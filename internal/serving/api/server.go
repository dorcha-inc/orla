// Package api provides the HTTP API for the Agentic Serving Layer daemon (RFC 5).
// The daemon is a control plane for coordination (shared context, cache policies,
// workflow execution). Actual inference happens locally on agents; the daemon
// coordinates state across multiple agents.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/dorcha-inc/orla/internal/serving"
	"go.uber.org/zap"
)

// writeJSON writes a JSON response to the http.ResponseWriter
func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		zap.L().Error("Failed to encode JSON response", zap.Error(err))
	}
}

// Server is the HTTP API server for the daemon
type Server struct {
	// servingLayer is the serving layer implementation
	servingLayer serving.ServingLayer
	// httpServer is the underlying HTTP server
	httpServer *http.Server
	// mux is the HTTP request multiplexer
	mux *http.ServeMux
}

// NewServer creates a new daemon API server
func NewServer(servingLayer serving.ServingLayer, listenAddress string) *Server {
	mux := http.NewServeMux()
	server := &Server{
		servingLayer: servingLayer,
		mux:          mux,
		httpServer: &http.Server{
			Addr:         listenAddress,
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
	}

	// Register routes
	server.registerRoutes()

	return server
}

// registerRoutes registers all API routes
func (s *Server) registerRoutes() {
	// Health check endpoint
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Context management endpoints
	s.mux.HandleFunc("POST /api/v1/context/sync", s.handleContextSync)
	s.mux.HandleFunc("GET /api/v1/context/", s.handleGetContext)

	// Workflow management endpoints
	s.mux.HandleFunc("POST /api/v1/workflow/start", s.handleWorkflowStart)
	s.mux.HandleFunc("GET /api/v1/workflow/task/next", s.handleGetNextTask)
	s.mux.HandleFunc("POST /api/v1/workflow/task/complete", s.handleCompleteTask)
	s.mux.HandleFunc("POST /api/v1/workflow/task/execute", s.handleExecuteTask)
	s.mux.HandleFunc("GET /api/v1/workflow/execution/", s.handleGetExecution)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	zap.L().Info("Starting daemon API server",
		zap.String("address", s.httpServer.Addr))
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, map[string]string{
		"status": "healthy",
	})
}

// ContextSyncRequest represents a request to sync context
type ContextSyncRequest struct {
	ServerName string          `json:"server_name"`
	Messages   []model.Message `json:"messages"`
}

// ContextSyncResponse represents the response from context sync
type ContextSyncResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// handleContextSync handles context synchronization requests
func (s *Server) handleContextSync(w http.ResponseWriter, r *http.Request) {
	var req ContextSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	// Get or create shared context for this server
	sharedCtx := s.servingLayer.GetSharedContext(req.ServerName)
	if sharedCtx == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, ContextSyncResponse{
			Success: false,
			Error:   fmt.Sprintf("no shared context for server '%s'", req.ServerName),
		})
		return
	}

	// Append messages to shared context
	for _, msg := range req.Messages {
		sharedCtx.AppendMessage(msg)
	}

	zap.L().Debug("Synced context from agent",
		zap.String("server_name", req.ServerName),
		zap.Int("message_count", len(req.Messages)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, ContextSyncResponse{
		Success: true,
	})
}

// GetContextResponse represents the response from get context
type GetContextResponse struct {
	Messages []model.Message `json:"messages"`
	Error    string          `json:"error,omitempty"`
}

// handleGetContext handles get context requests
func (s *Server) handleGetContext(w http.ResponseWriter, r *http.Request) {
	// Extract server name from URL path: /api/v1/context/{serverName}
	serverName := strings.TrimPrefix(r.URL.Path, "/api/v1/context/")
	if serverName == "" {
		http.Error(w, "server name required in URL path", http.StatusBadRequest)
		return
	}

	sharedCtx := s.servingLayer.GetSharedContext(serverName)
	if sharedCtx == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, GetContextResponse{
			Error: fmt.Sprintf("no shared context for server '%s'", serverName),
		})
		return
	}

	messages := sharedCtx.GetMessages()

	zap.L().Debug("Returned context to agent",
		zap.String("server_name", serverName),
		zap.Int("message_count", len(messages)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, GetContextResponse{
		Messages: messages,
	})
}

// WorkflowStartRequest represents a workflow start request
type WorkflowStartRequest struct {
	WorkflowName string `json:"workflow_name"`
}

// WorkflowStartResponse represents a workflow start response
type WorkflowStartResponse struct {
	ExecutionID string `json:"execution_id"`
	Error       string `json:"error,omitempty"`
}

// handleWorkflowStart handles workflow start requests
func (s *Server) handleWorkflowStart(w http.ResponseWriter, r *http.Request) {
	var req WorkflowStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	execution, err := s.servingLayer.StartWorkflow(ctx, req.WorkflowName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, WorkflowStartResponse{
			Error: err.Error(),
		})
		return
	}

	zap.L().Debug("Started workflow",
		zap.String("workflow_name", req.WorkflowName),
		zap.String("execution_id", execution.ExecutionID))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, WorkflowStartResponse{
		ExecutionID: execution.ExecutionID,
	})
}

// GetNextTaskResponse represents the response from get next task
type GetNextTaskResponse struct {
	Task      *config.WorkflowTask `json:"task,omitempty"`
	TaskIndex int                  `json:"task_index"`
	Complete  bool                 `json:"complete"`
	Error     string               `json:"error,omitempty"`
}

// handleGetNextTask handles get next task requests
func (s *Server) handleGetNextTask(w http.ResponseWriter, r *http.Request) {
	executionID := r.URL.Query().Get("execution_id")
	if executionID == "" {
		http.Error(w, "execution_id query parameter required", http.StatusBadRequest)
		return
	}

	execution, err := s.servingLayer.GetExecution(executionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, GetNextTaskResponse{
			Error: err.Error(),
		})
		return
	}

	// Check if workflow is complete
	if execution.CurrentTaskIndex >= len(execution.Tasks) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		writeJSON(w, GetNextTaskResponse{
			Complete: true,
		})
		return
	}

	task := execution.Tasks[execution.CurrentTaskIndex]

	zap.L().Debug("Returning next task",
		zap.String("execution_id", executionID),
		zap.Int("task_index", execution.CurrentTaskIndex))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, GetNextTaskResponse{
		Task:      task,
		TaskIndex: execution.CurrentTaskIndex,
		Complete:  false,
	})
}

// CompleteTaskRequest represents a request to complete a task
type CompleteTaskRequest struct {
	ExecutionID string          `json:"execution_id"`
	TaskIndex   int             `json:"task_index"`
	Response    *model.Response `json:"response,omitempty"`
}

// CompleteTaskResponse represents the response from completing a task
type CompleteTaskResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// handleCompleteTask handles task completion requests
func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	var req CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	execution, err := s.servingLayer.GetExecution(req.ExecutionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, CompleteTaskResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Verify task index matches
	if req.TaskIndex != execution.CurrentTaskIndex {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, CompleteTaskResponse{
			Success: false,
			Error:   fmt.Sprintf("task index mismatch: expected %d, got %d", execution.CurrentTaskIndex, req.TaskIndex),
		})
		return
	}

	// Update execution context with response
	if req.Response != nil && req.Response.Content != "" {
		execution.Context = append(execution.Context, model.Message{
			Role:    model.MessageRoleAssistant,
			Content: req.Response.Content,
		})
	}

	// Advance to next task
	execution.CurrentTaskIndex++
	if execution.CurrentTaskIndex >= len(execution.Tasks) {
		execution.State = serving.WorkflowExecutionStateCompleted
	}

	zap.L().Debug("Completed task",
		zap.String("execution_id", req.ExecutionID),
		zap.Int("task_index", req.TaskIndex),
		zap.Int("next_task_index", execution.CurrentTaskIndex))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, CompleteTaskResponse{
		Success: true,
	})
}

// ExecuteTaskRequest represents a request to execute a task
type ExecuteTaskRequest struct {
	ExecutionID string `json:"execution_id"`
	TaskIndex   int    `json:"task_index"`
	Prompt      string `json:"prompt"`
	MaxTokens   *int   `json:"max_tokens,omitempty"` // Optional: maximum tokens to generate
}

// ExecuteTaskResponse represents the response from executing a task
type ExecuteTaskResponse struct {
	Success  bool            `json:"success"`
	Response *model.Response `json:"response,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// handleExecuteTask handles task execution requests
// This endpoint executes inference AND applies cache policies
func (s *Server) handleExecuteTask(w http.ResponseWriter, r *http.Request) {
	var req ExecuteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	execution, err := s.servingLayer.GetExecution(req.ExecutionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, ExecuteTaskResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Verify task index matches
	if req.TaskIndex != execution.CurrentTaskIndex {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, ExecuteTaskResponse{
			Success: false,
			Error:   fmt.Sprintf("task index mismatch: expected %d, got %d", execution.CurrentTaskIndex, req.TaskIndex),
		})
		return
	}

	// Execute the task (this handles inference, context, and cache policies)
	ctx := r.Context()
	response, err := s.servingLayer.ExecuteTask(ctx, execution, req.TaskIndex, req.Prompt, req.MaxTokens)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, ExecuteTaskResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	zap.L().Debug("Executed task via API",
		zap.String("execution_id", req.ExecutionID),
		zap.Int("task_index", req.TaskIndex),
		zap.Int("response_length", len(response.Content)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, ExecuteTaskResponse{
		Success:  true,
		Response: response,
	})
}

// GetExecutionResponse represents the response from get execution
type GetExecutionResponse struct {
	Execution *serving.WorkflowExecution `json:"execution,omitempty"`
	Error     string                     `json:"error,omitempty"`
}

// handleGetExecution handles get execution requests
func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	// Extract execution ID from URL path: /api/v1/workflow/execution/{executionID}
	executionID := strings.TrimPrefix(r.URL.Path, "/api/v1/workflow/execution/")
	if executionID == "" {
		http.Error(w, "execution ID required in URL path", http.StatusBadRequest)
		return
	}

	execution, err := s.servingLayer.GetExecution(executionID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, GetExecutionResponse{
			Error: err.Error(),
		})
		return
	}

	zap.L().Debug("Returned execution",
		zap.String("execution_id", executionID),
		zap.String("state", string(execution.State)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, GetExecutionResponse{
		Execution: execution,
	})
}
