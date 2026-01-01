// Package agent implements the agent loop and MCP client for Orla Agent Mode (RFC 4).
package agent

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/model"
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

// ExecuteAgentPrompt is the main entry point for agent execution
// It handles the full flow: config loading, executor creation, context/signal handling, and execution
// prompt: the agent prompt as a single string (should be quoted when called from CLI)
func ExecuteAgentPrompt(prompt string, modelOverride string) error {

	// Load config
	cfg, configErr := config.LoadConfig("")
	if configErr != nil {
		return fmt.Errorf("failed to load config: %w", configErr)
	}

	// Override model if specified
	if modelOverride != "" {
		cfg.Model = modelOverride
	}

	// Create executor
	executor, executorErr := NewExecutor(cfg)
	if executorErr != nil {
		return fmt.Errorf("failed to create executor: %w", executorErr)
	}

	// Create context with cancellation and signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Ensure model is ready
	ensureReadyErr := executor.provider.EnsureReady(ctx)
	if ensureReadyErr != nil {
		return fmt.Errorf("model not ready: %w", ensureReadyErr)
	}

	// Create MCP client (connects to internal server)
	// Use empty string to use current executable
	mcpClient, clientErr := NewClient(ctx)
	if clientErr != nil {
		return fmt.Errorf("failed to create MCP client: %w", clientErr)
	}

	defer core.LogDeferredError(mcpClient.Close)

	// Create agent loop
	loop := NewLoop(mcpClient, executor.provider, cfg)

	// Create stream handler if streaming is enabled
	var streamHandler StreamHandler
	if cfg.Streaming {
		streamHandler = func(chunk string) error {
			fmt.Print(chunk)
			// Note: We don't call os.Stdout.Sync() here because:
			// 1. It's not necessary for most use cases (stdout is line-buffered by default)
			// 2. It can fail with "inappropriate ioctl for device" when stdout is redirected
			//    or is not a regular file (e.g., pipes, terminals in some configurations)
			// 3. The Go runtime handles buffering appropriately for interactive terminals
			return nil
		}
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

	// Print final response content
	// Note: If streaming was enabled and there were no tool calls, the response was already
	// printed via the stream handler. If there were tool calls, the final response after
	// tool calls was NOT streamed (we only stream iteration 0), so we need to print it.
	// We can't easily distinguish these cases, so we always print to ensure the final
	// response after tool calls is shown. This may cause slight duplication if streaming
	// was enabled and there were no tool calls, but that's acceptable.
	if response.Content != "" {
		fmt.Println(response.Content)
	}

	return nil
}
