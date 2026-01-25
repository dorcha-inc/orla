// Package agent implements the agent loop and MCP client for Orla Agent Mode (RFC 4).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/dorcha-inc/orla/internal/serving"
	"github.com/dorcha-inc/orla/internal/tui"
	publicapi "github.com/dorcha-inc/orla/pkg/api"
	"go.uber.org/zap"
)

// Executor handles agent execution with proper setup and teardown
type Executor struct {
	cfg      *config.OrlaConfig
	provider model.Provider
}

// NewExecutor creates a new agent executor
func NewExecutor(cfg *config.OrlaConfig) (*Executor, error) {
	// Create model provider
	provider, err := model.NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create model provider: %w", err)
	}

	return &Executor{
		cfg:      cfg,
		provider: provider,
	}, nil
}

// createStreamHandler creates a stream handler with state tracking for thinking/content transitions
func createStreamHandler(cfg *config.OrlaConfig) StreamHandler {
	var inThinking bool
	thinkingEnabled := cfg != nil && cfg.ShowThinking
	showToolCalls := cfg != nil && cfg.ShowToolCalls
	var toolNames []string
	var inToolCalls bool

	completeThinking := func() {
		if !thinkingEnabled {
			tui.ProgressSuccess("")
		} else {
			tui.Info("\ncompleted the think\n\n")
		}
		inThinking = false
	}

	completeToolCalls := func() {
		if inToolCalls && !showToolCalls {
			toolList := ""
			if len(toolNames) > 0 {
				toolList = strings.Join(toolNames, ", ")
			}
			tui.ProgressSuccess(fmt.Sprintf("calling tools: %s", toolList))
			toolNames = nil
			inToolCalls = false
		}
	}

	return func(event model.StreamEvent) error {
		switch e := event.(type) {
		case *model.ThinkingEvent:
			// Print "thinking:" prefix when thinking starts
			if !inThinking {
				inThinking = true
				if !thinkingEnabled {
					tui.Progress("having a think...")
					break
				}
				tui.Info("having a think:\n")
			}

			// When we are in thinking but thinking is disabled, break out of the loop
			if !thinkingEnabled {
				break
			}

			tui.ThinkingMessage(e.Content)
		case *model.ContentEvent:
			if inThinking {
				completeThinking()
			}
			if inToolCalls {
				completeToolCalls()
			}

			fmt.Print(e.Content)
		case *model.ToolCallEvent:
			if inThinking {
				completeThinking()
			}

			// Format tool call with params if available
			if e.Name == "" {
				return fmt.Errorf("tool call name is empty")
			}

			// Track tool names for progress message
			if !showToolCalls {
				if !inToolCalls {
					inToolCalls = true
				}
				// Add tool name if not already in list
				found := false
				for _, name := range toolNames {
					if name == e.Name {
						found = true
						break
					}
				}
				if !found {
					toolNames = append(toolNames, e.Name)
					toolList := strings.Join(toolNames, ", ")
					tui.Progress(fmt.Sprintf("calling tools: %s", toolList))
				}
				return nil
			}

			// Show detailed tool call info when enabled
			// Tool calls are metadata, so they go to stderr (consistent with thinking messages)
			if len(e.Arguments) == 0 {
				fmt.Fprintf(os.Stderr, "\ntool call received: %s\n", e.Name)
				return nil
			}

			argsJSON, err := json.MarshalIndent(e.Arguments, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal tool call arguments: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\ntool call received: %s\nparams: %s\n", e.Name, string(argsJSON))
		default:
			return fmt.Errorf("unknown stream event type: %T", e)
		}

		// Flush stdout to ensure immediate output
		syncErr := os.Stdout.Sync()
		if syncErr != nil {
			zap.L().Error("failed to flush stdout", zap.Error(syncErr))
		}
		return nil
	}
}

// readStdinIfAvailable reads from stdin if it's available (not a TTY)
// Returns the content and true if stdin was available, or empty string and false if not
func readStdinIfAvailable() (string, bool, error) {
	// Check if stdin is a terminal using the tui utility
	// If it's not a TTY, it means input is being piped or redirected
	if tui.IsTerminal(os.Stdin) {
		// Stdin is a TTY, no input available
		return "", false, nil
	}

	// Read all from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", false, fmt.Errorf("failed to read stdin: %w", err)
	}

	return string(data), true, nil
}

const daemonTimeout = 5 * time.Second

// createContextWithSignals creates a context with cancellation and signal handling
func createContextWithSignals() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	return ctx, cancel
}

// executeWithServingLayer handles Agentic Serving Layer integration (RFC 5)
// Returns (handled, error) where handled is true if execution was completed
func executeWithServingLayer(cfg *config.OrlaConfig, prompt string, daemonURL string, profileName string, workflowName string) (bool, error) {
	// Early return if neither workflow nor profile is specified
	if workflowName == "" && profileName == "" {
		return false, nil
	}

	// Validate that agentic serving config exists
	if cfg.AgenticServing == nil {
		return false, fmt.Errorf("agentic_serving configuration is required when using --daemon, --profile, or --workflow flags")
	}

	// Create context with signal handling
	ctx, cancel := createContextWithSignals()
	defer cancel()

	// Handle workflow execution
	if workflowName != "" {
		if daemonURL != "" {
			// Execute workflow with daemon coordination
			return executeWorkflowWithDaemon(ctx, cfg, prompt, daemonURL, workflowName)
		}
		// Execute workflow locally
		return executeWorkflowEmbedded(ctx, cfg, prompt, workflowName)
	}

	// Execute profile-based execution locally
	return executeWithProfile(ctx, cfg, prompt, profileName)
}

// executeWorkflowWithDaemon executes a workflow coordinated by a remote daemon
// Uses the public API workflow executor for remote execution
func executeWorkflowWithDaemon(ctx context.Context, _ *config.OrlaConfig, prompt string, daemonURL string, workflowName string) (bool, error) {
	zap.L().Info("Executing workflow with daemon coordination",
		zap.String("daemon_url", daemonURL),
		zap.String("workflow_name", workflowName))

	// Create workflow executor (daemon reads orla.yaml and resolves server names automatically)
	executor := publicapi.NewWorkflowExecutor(daemonURL)

	healthClient := publicapi.NewClient(daemonURL)
	healthCtx, healthCancel := context.WithTimeout(ctx, daemonTimeout)
	if err := healthClient.Health(healthCtx); err != nil {
		healthCancel()
		return false, fmt.Errorf("daemon health check failed: %w", err)
	}
	healthCancel()

	// Execute workflow using public API (remote execution - daemon handles inference)
	// Use callback to print task output
	err := executor.ExecuteWorkflowWithCallback(ctx, workflowName, prompt, 0, func(taskIndex int, task *publicapi.WorkflowTask, response *publicapi.TaskResponse) error {
		if response.Content != "" {
			fmt.Printf("\n[Task %d - %s]:\n%s\n", taskIndex+1, task.AgentProfile, response.Content)
		}
		return nil
	})

	if err != nil {
		return false, fmt.Errorf("workflow execution failed: %w", err)
	}

	return true, nil
}

// executeWorkflowEmbedded executes a workflow locally without daemon coordination
func executeWorkflowEmbedded(ctx context.Context, cfg *config.OrlaConfig, prompt string, workflowName string) (bool, error) {
	zap.L().Info("Executing workflow in embedded mode",
		zap.String("workflow_name", workflowName))

	// Create local serving layer
	layer, err := serving.NewLayer(cfg.AgenticServing)
	if err != nil {
		return false, fmt.Errorf("failed to create serving layer: %w", err)
	}

	// Start workflow
	execution, err := layer.StartWorkflow(ctx, workflowName)
	if err != nil {
		return false, fmt.Errorf("failed to start workflow: %w", err)
	}

	zap.L().Debug("Started workflow locally",
		zap.String("execution_id", execution.ExecutionID),
		zap.Int("task_count", len(execution.Tasks)))

	// Execute all tasks in the workflow
	currentPrompt := prompt
	for i, task := range execution.Tasks {
		// Determine the prompt for this task
		taskPrompt := currentPrompt
		if task.Prompt != "" {
			taskPrompt = task.Prompt
		}

		zap.L().Debug("Executing workflow task",
			zap.String("execution_id", execution.ExecutionID),
			zap.Int("task_index", i),
			zap.String("agent_profile", task.AgentProfile))

		// Execute task
		response, err := layer.ExecuteTask(ctx, execution, i, taskPrompt, nil)
		if err != nil {
			return false, fmt.Errorf("failed to execute workflow task %d: %w", i, err)
		}

		// Print task output
		if response != nil && response.Content != "" {
			fmt.Printf("\n[Task %d - %s]:\n%s\n", i+1, task.AgentProfile, response.Content)
			// Use response as input for next task if it uses context
			if i+1 < len(execution.Tasks) && execution.Tasks[i+1].UseContext {
				currentPrompt = response.Content
			}
		}
	}

	return true, nil
}

// executeWithProfile executes a single agent prompt using a configured profile
func executeWithProfile(ctx context.Context, cfg *config.OrlaConfig, prompt string, profileName string) (bool, error) {
	// Create local serving layer
	layer, err := serving.NewLayer(cfg.AgenticServing)
	if err != nil {
		return false, fmt.Errorf("failed to create serving layer: %w", err)
	}

	// Get provider from serving layer
	provider, err := layer.GetProvider(ctx, profileName, nil)
	if err != nil {
		return false, fmt.Errorf("failed to get provider for profile '%s': %w", profileName, err)
	}

	// Set show progress based on config
	if cfg != nil {
		tui.SetShowProgress(cfg.ShowProgress)
	}

	// Ensure model is ready
	tui.Progress("Ensuring model is ready...")
	if readyErr := provider.EnsureReady(ctx); readyErr != nil {
		return false, fmt.Errorf("model not ready: %w", readyErr)
	}
	tui.ProgressSuccess("Model ready")

	// Create MCP client
	tui.Progress("Connecting to tools...")
	mcpClient, err := NewClient(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to create MCP client: %w", err)
	}

	mcpTools, err := mcpClient.ListTools(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list tools: %w", err)
	}

	tui.ProgressSuccess(fmt.Sprintf("Connected to %d tools", len(mcpTools)))

	defer core.LogDeferredError(mcpClient.Close)

	// Create agent loop
	loop := NewLoop(mcpClient, provider, cfg)

	// Create stream handler if streaming is enabled
	var streamHandler StreamHandler
	if cfg.Streaming {
		streamHandler = createStreamHandler(cfg)
	}

	// Execute agent loop
	response, err := loop.Execute(ctx, prompt, nil, cfg.Streaming, streamHandler)
	if err != nil {
		return false, fmt.Errorf("agent execution failed: %w", err)
	}

	if response == nil {
		return false, fmt.Errorf("response is nil")
	}

	// Print newline after streaming (if streaming was enabled)
	if cfg.Streaming {
		fmt.Println()
		return true, nil
	}

	// Print thinking trace if present and enabled (non-streaming)
	if cfg.ShowThinking && response.Thinking != "" {
		tui.ThinkingMessage("thinking: ")
		tui.ThinkingMessage(response.Thinking)
		tui.Info("\n\n")
	}

	// Print final response content
	if response.Content != "" {
		rendered, err := tui.RenderMarkdown(response.Content, 80)
		if err == nil && rendered != response.Content {
			fmt.Print(rendered)
		} else {
			fmt.Println(response.Content)
		}
	}

	return true, nil
}

// ExecuteAgentPrompt is the main entry point for agent execution
// It handles the full flow: config loading, executor creation, context/signal handling, and execution
// prompt: the agent prompt as a single string (should be quoted when called from CLI)
// modelOverride: optional model override (for backward compatibility)
// daemonURL: optional daemon URL to connect to (RFC 5)
// profileName: optional agent profile name from config (RFC 5)
// workflowName: optional workflow name to execute (RFC 5)
func ExecuteAgentPrompt(prompt string, modelOverride string, daemonURL string, profileName string, workflowName string) error {
	if prompt == "" {
		return fmt.Errorf("prompt is required")
	}

	// Read stdin if available (piped input)
	// This makes commands like "summarize this" < file.txt work correctly
	stdinContent, hasStdin, err := readStdinIfAvailable()
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	// If stdin is available, include it in the prompt context
	if hasStdin && stdinContent != "" {
		// Enhance the prompt to include the stdin content
		// Format it clearly so the model understands this is the content to process
		// Use a separator to make it clear where the content begins
		prompt = fmt.Sprintf("%s\n\n--- Content from stdin ---\n%s\n--- End of content from stdin ---", prompt, stdinContent)
	}

	// Load config
	cfg, configErr := config.LoadConfig("")
	if configErr != nil {
		return fmt.Errorf("failed to load config: %w", configErr)
	}

	// Override model if specified (only if not using serving layer)
	if modelOverride != "" {
		cfg.Model = modelOverride
	}

	// Handle Agentic Serving Layer integration (RFC 5)
	handled, err := executeWithServingLayer(cfg, prompt, daemonURL, profileName, workflowName)
	if err != nil {
		return fmt.Errorf("failed to execute with serving layer: %w", err)
	}

	if handled {
		return nil // Execution was handled by serving layer
	}

	// Fall back to existing executor creation for backward compatibility
	executor, executorErr := NewExecutor(cfg)
	if executorErr != nil {
		return fmt.Errorf("failed to create executor: %w", executorErr)
	}

	// Create context with cancellation and signal handling
	ctx, cancel := createContextWithSignals()
	defer cancel()

	// Set show progress based on config
	if cfg != nil {
		tui.SetShowProgress(cfg.ShowProgress)
	}

	// Ensure model is ready
	tui.Progress("Ensuring model is ready...")
	ensureReadyErr := executor.provider.EnsureReady(ctx)
	if ensureReadyErr != nil {
		return fmt.Errorf("model not ready: %w", ensureReadyErr)
	}
	tui.ProgressSuccess("Model ready")

	// Create MCP client (connects to internal server)
	// Use empty string to use current executable
	tui.Progress("Connecting to tools...")
	mcpClient, clientErr := NewClient(ctx)
	if clientErr != nil {
		return fmt.Errorf("failed to create MCP client: %w", clientErr)
	}

	mcpTools, err := mcpClient.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	tui.ProgressSuccess(fmt.Sprintf("Connected to %d tools", len(mcpTools)))

	defer core.LogDeferredError(mcpClient.Close)

	// Create agent loop
	loop := NewLoop(mcpClient, executor.provider, cfg)

	// Create stream handler if streaming is enabled
	var streamHandler StreamHandler
	if cfg.Streaming {
		streamHandler = createStreamHandler(cfg)
	}

	// Execute agent loop (handles both streaming and non-streaming internally)
	response, executeErr := loop.Execute(ctx, prompt, nil, cfg.Streaming, streamHandler)
	if executeErr != nil {
		return fmt.Errorf("agent execution failed: %w", executeErr)
	}

	if response == nil {
		return fmt.Errorf("response is nil")
	}

	// Print newline after streaming (if streaming was enabled)
	if cfg.Streaming {
		fmt.Println()
		return nil
	}

	// Print thinking trace if present and enabled (non-streaming)
	if cfg.ShowThinking && response.Thinking != "" {
		tui.ThinkingMessage("thinking: ")
		tui.ThinkingMessage(response.Thinking)
		tui.Info("\n\n")
	}

	// Print final response content
	// Note: If streaming was enabled and there were no tool calls, the response was already
	// printed via the stream handler.
	if response.Content != "" {
		// Try to render as markdown if it looks like markdown
		rendered, err := tui.RenderMarkdown(response.Content, 80)
		if err == nil && rendered != response.Content {
			// Successfully rendered markdown
			fmt.Print(rendered)
		} else {
			// Plain text or rendering failed
			fmt.Println(response.Content)
		}
	}

	return nil
}
