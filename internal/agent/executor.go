// Package agent implements the agent loop and MCP client for Orla Agent Mode (RFC 4).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/model"
	"github.com/dorcha-inc/orla/internal/tui"
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

			fmt.Print(tui.RenderThinking(e.Content))
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
			if len(e.Arguments) == 0 {
				fmt.Printf("\ntool call received: %s\n", e.Name)
				return nil
			}

			argsJSON, err := json.MarshalIndent(e.Arguments, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal tool call arguments: %w", err)
			}
			fmt.Printf("\ntool call received: %s\nparams: %s\n", e.Name, string(argsJSON))
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
		fmt.Print(tui.RenderThinking("thinking: "))
		fmt.Print(tui.RenderThinking(response.Thinking))
		fmt.Print("\n\n")
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
