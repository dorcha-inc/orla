// Package agent implements the agent loop and MCP client for Orla Agent Mode (RFC 4).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/harvard-cns/orla/internal/config"
	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/tui"
	"log/slog"
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

// StreamHandler is a function that handles streaming events.
type StreamHandler func(event model.StreamEvent) error

// Execute runs one model call with the given prompt and optional message history,
// then returns the response. If stream is true and streamHandler is set, events
// are delivered to the handler. No tools are passed to the model.
func (e *Executor) Execute(ctx context.Context, prompt string, messages []model.Message, stream bool, streamHandler StreamHandler) (*model.Response, error) {
	if stream && streamHandler == nil {
		return nil, fmt.Errorf("stream handler is required when streaming is enabled")
	}

	conversation := make([]model.Message, len(messages))
	copy(conversation, messages)
	if prompt != "" {
		conversation = append(conversation, model.Message{
			Role:    model.MessageRoleUser,
			Content: prompt,
		})
	}

	slog.Debug("Agent execute",
		"prompt", prompt,
		"message_count", len(conversation))

	tui.Progress("Processing request")

	opts := model.InferenceOptions{Stream: stream}
	if e.cfg != nil && e.cfg.ShowThinking {
		opts.ReasoningEffort = "high"
	} else {
		opts.ReasoningEffort = "none"
	}
	response, streamCh, err := e.provider.Chat(ctx, conversation, nil, opts)
	if err != nil {
		return nil, fmt.Errorf("model chat failed: %w", err)
	}
	if response == nil {
		return nil, fmt.Errorf("received nil response from model")
	}
	if stream && streamCh == nil {
		return nil, fmt.Errorf("stream channel is nil but streaming is enabled")
	}

	if streamCh != nil {
		tui.ProgressSuccess("Stream started.")
		for event := range streamCh {
			if err := streamHandler(event); err != nil {
				return nil, fmt.Errorf("stream handler error: %w", err)
			}
		}
	}

	return response, nil
}

// createStreamHandler creates a stream handler with state tracking for thinking/content transitions
func createStreamHandler(cfg *config.OrlaConfig) StreamHandler {
	var inThinking bool
	thinkingEnabled := cfg != nil && cfg.ShowThinking
	showToolCalls := false
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
			// Ignore errors when stdout is not a TTY (e.g. pipe/redirect); Sync() can return
			// ENOTTY or EINVAL, often wrapped in *fs.PathError.
			var errno syscall.Errno
			if !errors.As(syncErr, &errno) || (errno != syscall.ENOTTY && errno != syscall.EINVAL) {
				slog.Error("failed to flush stdout", "error", syncErr)
			}
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

// ExecuteAgentPrompt is the main entry point for one-shot agent execution.
// configPath is the path to the config file; if empty, defaults only (no file read).
func ExecuteAgentPrompt(prompt string, modelOverride string, configPath string) error {
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
		prompt = fmt.Sprintf("%s\n\n--- Content from stdin ---\n%s\n--- End of content from stdin ---", prompt, stdinContent)
	}

	cfg, configErr := config.LoadConfig(configPath)
	if configErr != nil {
		return fmt.Errorf("failed to load config: %w", configErr)
	}

	// Re-initialize logger with config. Agent defaults to "error" when no config file
	// so regular runs produce clean output (no info/warn logs).
	logLevel := cfg.LogLevel
	if configPath == "" {
		logLevel = "error"
	}

	if initErr := core.InitLogger(cfg.LogFormat == config.OrlaLogFormatPretty, logLevel); initErr != nil {
		return fmt.Errorf("failed to initialize logger: %w", initErr)
	}

	if modelOverride != "" {
		cfg.Model = modelOverride
	}

	executor, executorErr := NewExecutor(cfg)
	if executorErr != nil {
		return fmt.Errorf("failed to create executor: %w", executorErr)
	}

	// Create context with cancellation and signal handling
	ctx, cancel := createContextWithSignals()
	defer cancel()

	if cfg != nil {
		tui.SetShowProgress(cfg.ShowProgress)
	}

	tui.Progress("Ensuring model is ready...")
	ensureReadyErr := executor.provider.EnsureReady(ctx)
	if ensureReadyErr != nil {
		return fmt.Errorf("model not ready: %w", ensureReadyErr)
	}
	tui.ProgressSuccess("Model ready")

	// Create stream handler if streaming is enabled
	var streamHandler StreamHandler
	if cfg.Streaming {
		streamHandler = createStreamHandler(cfg)
	}

	response, executeErr := executor.Execute(ctx, prompt, nil, cfg.Streaming, streamHandler)
	if executeErr != nil {
		return fmt.Errorf("agent execution failed: %w", executeErr)
	}

	if response == nil {
		return fmt.Errorf("response is nil")
	}

	if cfg.Streaming {
		fmt.Println()
		return nil
	}

	if cfg.ShowThinking && response.Thinking != "" {
		tui.ThinkingMessage("thinking: ")
		tui.ThinkingMessage(response.Thinking)
		tui.Info("\n\n")
	}

	if response.Content == "" {
		slog.Warn("did not receive a response from the model")
		return nil
	}

	// Print final response content
	// Note: If streaming was enabled, the response was already printed via the stream handler.
	// Try to render as markdown if it looks like markdown
	rendered, err := tui.RenderMarkdown(response.Content, 80)
	if err != nil {
		slog.Warn("failed to render markdown, falling back to plain text", "error", err)
		fmt.Println(response.Content)
		return nil
	}

	fmt.Print(rendered)
	return nil
}
