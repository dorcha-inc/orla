// Package serving implements the Agentic Serving Layer (RFC 5).
package serving

import (
	"context"
	"fmt"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"go.uber.org/zap"
)

// Layer is the concrete implementation of ServingLayer
type Layer struct {
	// cfg is the agentic serving configuration
	cfg *config.AgenticServingConfig
	// serverManager manages LLM server configurations and providers
	serverManager *LLMServerManager
	// contextManager manages shared contexts
	contextManager *ContextManager
	// cacheManager manages cache policies
	cacheManager *CacheManager
	// workflowExecutor executes workflows
	workflowExecutor *WorkflowExecutor
	// agentProfiles maps profile names to their configurations
	agentProfiles map[string]*config.AgentProfile
}

// NewLayer creates a new serving layer
func NewLayer(cfg *config.AgenticServingConfig) (*Layer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("agentic serving configuration is required")
	}

	// Create server manager
	serverManager := NewLLMServerManager(cfg.LLMServers)

	// Create context manager
	contextManager := NewContextManager()

	// Initialize shared contexts for servers with shared context enabled
	for _, server := range cfg.LLMServers {
		if server.Context != nil && server.Context.Shared {
			syncInterval := server.Context.SyncInterval
			if syncInterval <= 0 {
				syncInterval = 100 // Default sync interval
			}
			contextManager.GetOrCreateSharedContext(server.Name, syncInterval)
		}
	}

	// Create cache policy evaluator and manager
	evaluator := NewCachePolicyEvaluator(cfg.LLMServers)
	cacheManager := NewCacheManager(evaluator)

	// Create workflow executor
	workflowExecutor := NewWorkflowExecutor(cfg.Workflows)

	// Index agent profiles by name
	agentProfiles := make(map[string]*config.AgentProfile)
	for _, profile := range cfg.AgentProfiles {
		agentProfiles[profile.Name] = profile
	}

	return &Layer{
		cfg:              cfg,
		serverManager:    serverManager,
		contextManager:   contextManager,
		cacheManager:     cacheManager,
		workflowExecutor: workflowExecutor,
		agentProfiles:    agentProfiles,
	}, nil
}

// GetProvider returns a model provider for an agent profile or workflow task
func (l *Layer) GetProvider(ctx context.Context, profileName string, task *config.WorkflowTask) (model.Provider, error) {
	// Get agent profile
	profile, exists := l.agentProfiles[profileName]
	if !exists {
		return nil, fmt.Errorf("agent_profile '%s' not found", profileName)
	}

	// Determine which LLM server to use
	serverName := profile.LLMServer
	if task != nil && task.LLMServer != "" {
		// Task has an LLM server override
		serverName = task.LLMServer
	}

	// Get provider from server manager
	provider, err := l.serverManager.GetProvider(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for llm_server '%s': %w", serverName, err)
	}

	zap.L().Debug("Got provider for agent profile",
		zap.String("profile_name", profileName),
		zap.String("server_name", serverName))

	return provider, nil
}

// StartWorkflow initializes a workflow execution
func (l *Layer) StartWorkflow(ctx context.Context, workflowName string) (*WorkflowExecution, error) {
	return l.workflowExecutor.StartWorkflow(ctx, workflowName)
}

// ExecuteTask executes a single workflow task with the given prompt
// It handles shared context, cache policies, and returns the response
func (l *Layer) ExecuteTask(ctx context.Context, execution *WorkflowExecution, taskIndex int, prompt string, maxTokens *int) (*model.Response, error) {
	if taskIndex < 0 || taskIndex >= len(execution.Tasks) {
		return nil, fmt.Errorf("invalid task index: %d (workflow has %d tasks)", taskIndex, len(execution.Tasks))
	}

	task := execution.Tasks[taskIndex]
	profileName := task.AgentProfile

	// Get agent profile to determine the LLM server
	profile, exists := l.agentProfiles[profileName]
	if !exists {
		return nil, fmt.Errorf("agent_profile '%s' not found", profileName)
	}

	// Determine which LLM server to use
	serverName := profile.LLMServer
	if task.LLMServer != "" {
		serverName = task.LLMServer
	}

	// Get provider for this task
	provider, err := l.GetProvider(ctx, profileName, task)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for task %d: %w", taskIndex, err)
	}

	// Determine the prompt to use
	taskPrompt := prompt
	if task.Prompt != "" {
		taskPrompt = task.Prompt
	}

	// Build messages for the chat request
	var messages []model.Message

	// If task uses context, include accumulated context from previous tasks
	if task.UseContext && len(execution.Context) > 0 {
		messages = append(messages, execution.Context...)
	}

	// Check if this server has shared context enabled
	serverCfg, err := l.serverManager.GetServerConfig(serverName)
	if err != nil {
		// NOTE(jadidbourbaki): this is not a blocking error, this can actually happen if we are not
		// sharing context with the daemon, in which case we just continue without shared context
		zap.L().Debug("Failed to get server config for server, continuing without shared context", zap.String("server_name", serverName))
	}

	if serverCfg != nil && serverCfg.Context != nil && serverCfg.Context.Shared {
		// Get shared context and include it
		sharedCtx := l.contextManager.GetSharedContext(serverName)
		if sharedCtx != nil {
			sharedMessages := sharedCtx.GetMessages()
			// Only add shared context if task doesn't already have context from execution
			if !task.UseContext || len(execution.Context) == 0 {
				messages = append(messages, sharedMessages...)
			}
		}
	}

	// Add the user message with the prompt
	userMsg := model.Message{
		Role:    model.MessageRoleUser,
		Content: taskPrompt,
	}
	messages = append(messages, userMsg)

	// Update execution state to running
	execution.State = WorkflowExecutionStateRunning

	// Execute inference (non-streaming for workflow tasks)
	response, _, err := provider.Chat(ctx, messages, nil, false, maxTokens)
	if err != nil {
		execution.State = WorkflowExecutionStateFailed
		return nil, fmt.Errorf("inference failed for task %d: %w", taskIndex, err)
	}

	// Update accumulated context with user message and response
	execution.Context = append(execution.Context, userMsg)
	if response.Content != "" {
		assistantMsg := model.Message{
			Role:    model.MessageRoleAssistant,
			Content: response.Content,
		}
		execution.Context = append(execution.Context, assistantMsg)
	}

	// Update shared context if enabled
	if serverCfg != nil && serverCfg.Context != nil && serverCfg.Context.Shared {
		sharedCtx := l.contextManager.GetOrCreateSharedContext(serverName, serverCfg.Context.SyncInterval)
		sharedCtx.AppendMessage(userMsg)
		if response.Content != "" {
			sharedCtx.AppendMessage(model.Message{
				Role:    model.MessageRoleAssistant,
				Content: response.Content,
			})
		}
	}

	// Evaluate cache policy
	isFinalTask := taskIndex == len(execution.Tasks)-1
	// For now, use a simple token estimate based on content length
	// TODO(jadidbourbaki): Use actual tokenizer for more accurate token counting
	turnSize := len(taskPrompt) + len(response.Content)
	shouldFlush, err := l.cacheManager.ShouldFlush(ctx, serverName, turnSize, 0.0, isFinalTask)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate cache policy: %w", err)
	}

	// Actually flush the cache
	if shouldFlush {
		flushErr := l.cacheManager.FlushCache(ctx, l.serverManager, serverName)
		if flushErr != nil {
			return nil, fmt.Errorf("failed to flush cache: %w", flushErr)
		}
	}

	// Advance task index
	execution.CurrentTaskIndex = taskIndex + 1
	if execution.CurrentTaskIndex >= len(execution.Tasks) {
		execution.State = WorkflowExecutionStateCompleted
	}

	zap.L().Debug("Executed workflow task",
		zap.String("workflow_name", execution.WorkflowName),
		zap.String("execution_id", execution.ExecutionID),
		zap.Int("task_index", taskIndex),
		zap.String("agent_profile", profileName),
		zap.Int("response_length", len(response.Content)))

	return response, nil
}

// GetSharedContext returns shared context for a given LLM server
func (l *Layer) GetSharedContext(serverName string) *SharedContext {
	return l.contextManager.GetSharedContext(serverName)
}

// GetExecution returns a workflow execution by ID
func (l *Layer) GetExecution(executionID string) (*WorkflowExecution, error) {
	return l.workflowExecutor.GetExecution(executionID)
}
