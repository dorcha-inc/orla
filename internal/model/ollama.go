package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultOllamaHost         = "http://localhost:11434"
	defaultOllamaTimeout      = 10 * time.Minute
	defaultOllamaStartTimeout = 5 * time.Minute
	defaultOllamaTemperature  = 0.7
	// defaultStreamBufferSize is the buffer size for streaming response channels
	// This allows the producer (HTTP stream reader) to get slightly ahead of the consumer
	// without blocking, while keeping memory usage reasonable
	defaultStreamBufferSize   = 255
	ollamaHealthCheckEndpoint = "/api/tags"
	ollamaChatEndpoint        = "/api/chat"
)

// OllamaProvider implements the Provider interface for Ollama
type OllamaProvider struct {
	modelName string
	baseURL   string
	client    *http.Client
	cfg       *config.OrlaConfig

	// Process tracking
	processMu sync.Mutex
	process   *exec.Cmd
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(modelName string, cfg *config.OrlaConfig) (*OllamaProvider, error) {
	baseURL := defaultOllamaHost
	if envURL := core.GetEnv("OLLAMA_HOST"); envURL != "" {
		baseURL = envURL
	}

	return &OllamaProvider{
		modelName: modelName,
		baseURL:   baseURL,
		client: &http.Client{
			Timeout: defaultOllamaTimeout,
		},
		cfg: cfg,
	}, nil
}

// SetTimeout sets the timeout for the Ollama provider
func (p *OllamaProvider) SetTimeout(timeout time.Duration) {
	p.client.Timeout = timeout
}

// Name returns the provider name
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// EnsureReady ensures Ollama is running and ready
func (p *OllamaProvider) EnsureReady(ctx context.Context) error {
	// Check if Ollama is already running
	running, err := p.isRunning()
	if err != nil {
		if errors.Is(err, ErrOllamaNotInstalled) {
			return fmt.Errorf("ollama is not installed. Please install Ollama: https://ollama.ai")
		}
		return fmt.Errorf("failed to check if Ollama is running: %w", err)
	}

	if running {
		zap.L().Debug("Ollama is already running")
		return nil
	}

	// If auto-start is disabled, return error
	if !p.cfg.AutoStartOllama {
		return fmt.Errorf("ollama is not running and auto_start_ollama is disabled. Please start Ollama manually")
	}

	// Try to start Ollama
	if err := p.startOllama(ctx); err != nil {
		return fmt.Errorf("failed to start Ollama: %w. Please start Ollama manually or enable auto_start_ollama", err)
	}

	// Wait for Ollama to be ready
	return p.waitForReady(ctx, defaultOllamaStartTimeout)
}

// Chat sends a chat request to Ollama
func (p *OllamaProvider) Chat(ctx context.Context, messages []Message, tools []*mcp.Tool, stream bool) (*Response, <-chan string, error) {
	// Ensure Ollama is ready
	if err := p.EnsureReady(ctx); err != nil {
		return nil, nil, err
	}

	// Convert messages to Ollama format
	ollamaMessages := make([]ollamaMessage, len(messages))
	for i := range messages {
		msg := ollamaMessage{
			Role:    string(messages[i].Role),
			Content: messages[i].Content,
		}
		// Add tool_name if this is a tool message
		if messages[i].Role == MessageRoleTool && messages[i].ToolName != "" {
			msg.ToolName = messages[i].ToolName
		}
		ollamaMessages[i] = msg
	}

	// Build request
	reqBody := ollamaChatRequest{
		Model:    p.modelName,
		Messages: ollamaMessages,
		Stream:   stream,
		Options: ollamaOptions{
			Temperature: defaultOllamaTemperature,
		},
	}

	// Add tools if provided (Ollama supports tool calling natively)
	if len(tools) > 0 {
		reqBody.Tools = convertToolsToOllamaFormat(tools)
		// Don't set Format=json when using tools - tools enable tool calling automatically
		// Format=json is only for structured outputs without tools
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s%s", p.baseURL, ollamaChatEndpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)

		// Note(jadidbourbaki): a bit of a misnomer using this function here.
		core.LogDeferredError(resp.Body.Close)

		if readErr != nil {
			return nil, nil, fmt.Errorf("ollama API error: %d (failed to read response body: %w)", resp.StatusCode, readErr)
		}
		return nil, nil, fmt.Errorf("ollama API error: %d - %s", resp.StatusCode, string(body))
	}

	if stream {
		// For streaming, accumulate response while streaming content
		response, streamCh := p.handleStreamResponse(resp.Body)
		return response, streamCh, nil
	}

	// For non-streaming, close the body when done
	defer core.LogDeferredError(resp.Body.Close)

	// Handle non-streaming response
	var ollamaResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	zap.L().Debug("Ollama response received",
		zap.String("content", ollamaResp.Message.Content),
		zap.Int("tool_calls_count", len(ollamaResp.Message.ToolCalls)),
		zap.Bool("has_tool_calls", ollamaResp.Message.ToolCalls != nil))

	response := &Response{
		Content: ollamaResp.Message.Content,
	}

	// Parse tool calls if present
	if len(ollamaResp.Message.ToolCalls) > 0 {
		response.ToolCalls = convertOllamaToolCalls(ollamaResp.Message.ToolCalls)
		zap.L().Debug("Parsed tool calls", zap.Int("count", len(response.ToolCalls)))
	} else if len(tools) > 0 && ollamaResp.Message.Content != "" {
		// If tools were provided but tool_calls is empty, check if content contains tool call JSON
		// Some models return tool calls as JSON in content when Format=json
		zap.L().Debug("No tool_calls field but tools were provided, content may contain tool call JSON")
	}

	return response, nil, nil
}

var ErrOllamaNotInstalled = errors.New("ollama is not installed")

// isRunning checks if Ollama is running
func (p *OllamaProvider) isRunning() (bool, error) {
	// First check if ollama command exists
	_, lookPathErr := exec.LookPath("ollama")
	if lookPathErr != nil {
		return false, ErrOllamaNotInstalled
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s%s", p.baseURL, ollamaHealthCheckEndpoint), nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Connection errors mean Ollama is not running (not an error condition)
		return false, nil
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		// Non-200 status means Ollama is not running properly (not an error condition)
		return false, nil
	}

	return true, nil
}

// startOllama attempts to start Ollama
func (p *OllamaProvider) startOllama(ctx context.Context) error {
	p.processMu.Lock()
	defer p.processMu.Unlock()

	// Check if we already have a process running
	if p.process != nil && p.process.Process != nil {
		// Check if the process is still alive by checking its state
		// If the process has exited, ProcessState will be non-nil
		if p.process.ProcessState == nil {
			// Process is still running
			zap.L().Debug("Ollama process already running, reusing existing process")
			return nil
		}
		// Process has exited, clear it
		p.process = nil
	}

	zap.L().Info("Attempting to start Ollama...")

	// First, try to check if Ollama is installed
	if _, err := exec.LookPath("ollama"); err != nil {
		return fmt.Errorf("ollama command not found. Please install Ollama: https://ollama.ai")
	}

	// Try to start Ollama as a background process
	cmd := exec.CommandContext(ctx, "ollama", "serve")

	// Start in background (don't wait for it)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Ollama: %w. Please start Ollama manually: ollama serve", err)
	}

	// Store the process reference
	p.process = cmd

	// Don't wait for the command - let it run in background
	go func() {
		if waitErr := cmd.Wait(); waitErr != nil {
			zap.L().Debug("Ollama process exited", zap.Error(waitErr))
		}
		// Clear the process reference when it exits
		p.processMu.Lock()
		if p.process == cmd {
			p.process = nil
		}
		p.processMu.Unlock()
	}()

	zap.L().Info("Ollama process started, waiting for it to become ready...")
	return nil
}

// waitForReady waits for Ollama to become ready
func (p *OllamaProvider) waitForReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			running, err := p.isRunning()
			if err != nil {
				// Continue waiting if there's an error (might be transient)
				continue
			}
			if running {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for Ollama to become ready")
			}
		}
	}
}

// handleStreamResponse handles streaming responses from Ollama
// It returns both a Response object (with accumulated content and tool calls) and a channel for streaming content
// The Response object is updated as chunks arrive, and tool calls are populated when the stream completes
// The response is accessed concurrently: the goroutine writes to it while the caller reads after the channel closes.
// To ensure proper synchronization, we use a mutex to protect writes, and the channel close provides
// a happens-before guarantee that all writes are complete before the caller reads.
func (p *OllamaProvider) handleStreamResponse(body io.ReadCloser) (*Response, <-chan string) {
	ch := make(chan string, defaultStreamBufferSize)
	response := &Response{
		Content:   "",
		ToolCalls: []ToolCallWithID{},
	}

	go func() {
		defer close(ch)                         // Channel close provides synchronization point
		defer core.LogDeferredError(body.Close) // Close the body when streaming is done
		decoder := json.NewDecoder(body)
		chunkCount := 0
		contentChunks := 0
		var accumulatedToolCalls []ollamaToolCall

		for {
			var chunk ollamaChatResponse

			decodeErr := decoder.Decode(&chunk)
			if decodeErr != nil {
				if errors.Is(decodeErr, io.EOF) {
					break
				}
				zap.L().Error("Failed to decode stream chunk", zap.Error(decodeErr))
				break
			}

			chunkCount++

			// Accumulate content
			if chunk.Message.Content != "" {
				response.Content += chunk.Message.Content
				contentChunks++
				ch <- chunk.Message.Content
			}

			// Accumulate tool calls if present in this chunk
			if len(chunk.Message.ToolCalls) > 0 {
				accumulatedToolCalls = append(accumulatedToolCalls, chunk.Message.ToolCalls...)
				zap.L().Debug("Tool calls found in stream chunk",
					zap.Int("chunk_num", chunkCount),
					zap.Int("tool_calls_count", len(chunk.Message.ToolCalls)),
					zap.Int("total_accumulated", len(accumulatedToolCalls)))
			}

			if chunk.Done {
				zap.L().Debug("Stream done flag received",
					zap.Int("total_chunks", chunkCount),
					zap.Int("content_chunks", contentChunks),
					zap.Int("accumulated_tool_calls", len(accumulatedToolCalls)))
				break
			}
		}

		// Convert accumulated tool calls to our format (this happens after stream completes)
		// The channel close below provides a synchronization point ensuring this write
		// is visible to readers after they consume the closed channel.
		if len(accumulatedToolCalls) > 0 {
			response.ToolCalls = convertOllamaToolCalls(accumulatedToolCalls)
			zap.L().Debug("Parsed tool calls from stream",
				zap.Int("count", len(response.ToolCalls)))
		}

		if contentChunks == 0 {
			zap.L().Warn("Stream completed but no content chunks were received",
				zap.Int("total_chunks_decoded", chunkCount))
		}
		// Channel close happens after all writes, providing a synchronization point.
		// The executor reads response.ToolCalls only after consuming the closed channel,
		// which ensures proper ordering per Go's memory model (close happens-before receive
		// that returns zero value from closed channel).
	}()
	return response, ch
}

// Ollama-specific types
type ollamaMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	ToolName string `json:"tool_name,omitempty"` // Required when role is "tool"
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Format   string          `json:"format,omitempty"`
}

type ollamaChatResponse struct {
	Message ollamaResponseMessage `json:"message"`
	Done    bool                  `json:"done"`
}

type ollamaResponseMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaToolCall struct {
	Type     string                 `json:"type"` // "function"
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Index     *int   `json:"index,omitempty"` // Optional index for parallel calls
	Name      string `json:"name"`
	Arguments any    `json:"arguments"` // Can be object or JSON string
}

// convertToolsToOllamaFormat converts mcp.Tool slice to Ollama format
func convertToolsToOllamaFormat(tools []*mcp.Tool) []ollamaTool {
	ollamaTools := make([]ollamaTool, len(tools))
	for i, tool := range tools {
		// Convert InputSchema from any to map[string]any
		var params map[string]any
		if tool.InputSchema != nil {
			if schemaMap, ok := tool.InputSchema.(map[string]any); ok {
				params = schemaMap
			} else {
				// If it's not a map, try to convert it
				params = make(map[string]any)
				zap.L().Warn("Tool InputSchema is not a map[string]any, using empty schema", zap.String("tool", tool.Name))
			}
		} else {
			params = make(map[string]any)
		}

		ollamaTools[i] = ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		}
	}
	return ollamaTools
}

// convertOllamaToolCalls converts Ollama tool calls to our format
func convertOllamaToolCalls(ollamaCalls []ollamaToolCall) []ToolCallWithID {
	toolCalls := make([]ToolCallWithID, len(ollamaCalls))
	for i, call := range ollamaCalls {
		var args map[string]any

		// Handle arguments - can be object or JSON string
		switch v := call.Function.Arguments.(type) {
		case map[string]any:
			// Already an object
			args = v
		case string:
			// JSON string, unmarshal it
			if err := json.Unmarshal([]byte(v), &args); err != nil {
				zap.L().Warn("Failed to parse tool call arguments",
					zap.String("tool", call.Function.Name),
					zap.Error(err))
				args = make(map[string]any)
			}
		default:
			// Try to marshal/unmarshal as fallback
			jsonBytes, err := json.Marshal(v)
			if err == nil {
				if err := json.Unmarshal(jsonBytes, &args); err != nil {
					zap.L().Warn("Failed to parse tool call arguments",
						zap.String("tool", call.Function.Name),
						zap.Error(err))
					args = make(map[string]any)
				}
			} else {
				zap.L().Warn("Failed to marshal tool call arguments",
					zap.String("tool", call.Function.Name),
					zap.Error(err))
				args = make(map[string]any)
			}
		}

		// Use index if provided, otherwise use position
		id := fmt.Sprintf("call_%d", i)
		if call.Function.Index != nil {
			id = fmt.Sprintf("call_%d", *call.Function.Index)
		}

		toolCalls[i] = ToolCallWithID{
			ID: id,
			McpCallToolParams: mcp.CallToolParams{
				Name:      call.Function.Name,
				Arguments: args,
			},
		}
	}
	return toolCalls
}
