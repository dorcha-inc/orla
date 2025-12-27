// Package server implements the Orla MCP server.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// OrlaServer stores the state and dependencies for the Orla MCP server.
type OrlaServer struct {
	config        *state.OrlaConfig
	configPath    string
	executor      *core.OrlaToolExecutor
	orlaMCPserver *mcp.Server
	mu            sync.RWMutex
	httpHandler   *mcp.StreamableHTTPHandler
}

// NewOrlaServer creates a new OrlaServer instance
func NewOrlaServer(cfg *state.OrlaConfig, configPath string) *OrlaServer {
	executor := core.NewOrlaToolExecutor(cfg.Timeout)

	orlaServer := &OrlaServer{
		config:     cfg,
		configPath: configPath,
		executor:   executor,
	}

	orlaServer.rebuildServer()

	// Create HTTP handler that manages sessions, Origin validation, etc.
	orlaServer.httpHandler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server {
			orlaServer.mu.RLock()
			defer orlaServer.mu.RUnlock()
			return orlaServer.orlaMCPserver
		},
		&mcp.StreamableHTTPOptions{
			Stateless: false,
		},
	)

	return orlaServer
}

// rebuildServer rebuilds OrlaServer's state with current tools.
// It uses the config already loaded in o.config (which should be up-to-date
// if called from Reload(), or set during New()).
func (o *OrlaServer) rebuildServer() {
	// note(jadidbourbaki): we lock to ensure atomic replacement of the server instance
	// which may happen due to multiple SIGHUP signals. This prevents partial updates
	// if rebuildServer is called concurrently.
	o.mu.Lock()
	defer o.mu.Unlock()

	// Create new Orla MCP server.
	// note(jadidbourbaki): this does *not* break existing connections, because
	// each connection handler (handleTCPConnection/ServeStdio) captures a reference
	// to the server instance before calling server.Run(). Once captured, that
	// connection uses that server instance for its lifetime. Old server instances
	// remain alive as long as active connections reference them (via goroutines).
	// New connections accepted after this point will use this new server instance.
	o.orlaMCPserver = mcp.NewServer(
		&mcp.Implementation{Name: "orla", Version: "1.0.0"},
		nil,
	)

	// Use the tools registry loaded from config (state.Load builds it)
	tools := o.config.ToolsRegistry
	toolList := tools.ListTools()

	// Log tool discovery results
	if len(toolList) == 0 {
		zap.L().Warn("No tools found in tools directory",
			zap.String("directory", o.config.ToolsDir),
			zap.String("hint", "Add executable files to the tools directory to enable MCP tools"))
	} else {
		zap.L().Info("Discovered tools",
			zap.Int("count", len(toolList)),
			zap.String("directory", o.config.ToolsDir))
	}

	// Register each discovered tool
	for i, tool := range toolList {
		zap.L().Info("Registering tool with MCP server",
			zap.Int("index", i),
			zap.String("name", tool.Name),
			zap.String("path", tool.Path),
			zap.String("description", tool.Description))
		o.registerTool(tool)
	}
}

// registerTool registers a single tool with the MCP server
func (o *OrlaServer) registerTool(tool *core.ToolEntry) {
	// Create a handler function for this tool using map[string]any for input
	// Wrap with panic recovery at the handler boundary since this is the single point
	// where we can return proper MCP error responses
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input map[string]any) (
		result *mcp.CallToolResult,
		output map[string]any,
		err error,
	) {
		// Panic recovery at the handler boundary
		defer func() {
			if r := recover(); r != nil {
				core.LogPanicRecovery("tool handler", r)
				// Return error response on panic
				result = &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("internal error: panic recovered in tool execution: %v", r),
						},
					},
				}
				output = nil
				err = fmt.Errorf("panic recovered: %v", r)
			}
		}()
		return o.handleToolCall(ctx, tool, input)
	}

	// Add tool to MCP server
	zap.L().Debug("Calling mcp.AddTool",
		zap.String("tool", tool.Name),
		zap.String("description", tool.Description))

	mcpTool := &mcp.Tool{
		Name:        tool.Name,
		Description: tool.Description,
	}

	// Add input schema if available
	if tool.InputSchema != nil {
		mcpTool.InputSchema = tool.InputSchema
	}

	// Add output schema if available
	if tool.OutputSchema != nil {
		mcpTool.OutputSchema = tool.OutputSchema
	}

	mcp.AddTool(o.orlaMCPserver, mcpTool, handler)
	zap.L().Debug("mcp.AddTool completed", zap.String("tool", tool.Name))
}

func wrapOutputDefault(output map[string]any, result *core.OrlaToolExecutionResult) map[string]any {
	output["stdout"] = result.Stdout
	output["stderr"] = result.Stderr
	output["exit_code"] = result.ExitCode
	return output
}

// handleToolCall handles a tool execution request
func (o *OrlaServer) handleToolCall(
	ctx context.Context,
	tool *core.ToolEntry,
	input map[string]any,
) (*mcp.CallToolResult, map[string]any, error) {

	startTime := time.Now()

	// Convert input map to arguments
	var args []string
	stdin := ""

	for k, v := range input {
		if k == "stdin" {
			if stdinVal, ok := v.(string); ok {
				stdin = stdinVal
			}
			continue
		}
		// Convert underscores to hyphens for command-line arguments (standard convention)
		argName := strings.ReplaceAll(k, "_", "-")
		args = append(args, fmt.Sprintf("--%s", argName))
		args = append(args, fmt.Sprintf("%v", v))
	}

	// Execute tool
	result, err := o.executor.Execute(ctx, tool, args, stdin)
	duration := time.Since(startTime).Seconds()

	if err != nil {
		core.LogToolExecution(tool.Name, duration, err)
		// Provide more helpful error messages
		errorMsg := fmt.Sprintf("Tool execution failed: %v", err)
		if result != nil && result.Error != nil {
			// Check for timeout errors and provide helpful message
			timeoutErrMsg := result.Error.Error()
			if strings.Contains(timeoutErrMsg, "timed out") {
				errorMsg = fmt.Sprintf("Tool '%s' timed out after %d seconds. Consider increasing the 'timeout' value in your configuration file.", tool.Name, o.config.Timeout)
			}
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: errorMsg,
				},
			},
		}, nil, nil
	}

	core.LogToolExecution(tool.Name, duration, result.Error)

	// Build response content
	content := []mcp.Content{
		&mcp.TextContent{
			Text: result.Stdout,
		},
	}

	if result.Stderr != "" {
		content = append(content, &mcp.TextContent{
			Text: fmt.Sprintf("stderr: %s", result.Stderr),
		})
	}

	if result.ExitCode != 0 {
		content = append(content, &mcp.TextContent{
			Text: fmt.Sprintf("exit_code: %d", result.ExitCode),
		})
	}

	// this includes deadline exceeded errors
	isError := result.Error != nil || result.ExitCode != 0

	callToolResult := &mcp.CallToolResult{
		IsError: isError,
		Content: content,
	}

	outputMap := make(map[string]any)

	if result.Error != nil {
		outputMap["error"] = result.Error.Error()
	}

	if tool.OutputSchema == nil {
		outputMap = wrapOutputDefault(outputMap, result)
		return callToolResult, outputMap, nil
	}

	// If tool has an output schema, try to parse stdout as JSON and use it as structured output
	// Note(jadidbourbaki): if we reach here, we have a valid output schema and a non-empty stdout

	var parsedOutput any
	err = json.Unmarshal([]byte(result.Stdout), &parsedOutput)

	if err != nil {
		zap.L().Error("Failed to parse tool stdout as JSON",
			zap.String("tool", tool.Name),
			zap.String("stdout", result.Stdout),
			zap.Error(err))
		// If we have an OutputSchema, we can't fall back to wrapper format
		// because it won't match the schema. Return an error instead.
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Tool output is not valid JSON: %v", err),
				},
			},
		}, nil, nil
	}

	// Successfully parsed JSON. If it's a map, use it directly
	// Otherwise wrap it. Output schemas typically define objects.
	parsedMap, ok := parsedOutput.(map[string]any)

	// Note(jadidbourbaki): this is unlikely to happen, but we handle it gracefully
	if !ok {
		zap.L().Error("Tool output is not a map",
			zap.String("tool", tool.Name),
			zap.String("stdout", result.Stdout))
		// If we have an OutputSchema, we can't fall back to wrapper format
		// because it won't match the schema. Return an error instead.
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: "Tool output is not a JSON object",
				},
			},
		}, nil, nil
	}

	outputMap = parsedMap

	// Note(jadidbourbaki): After some experimentation, it seems like we
	// should not set StructuredContent manually, the MCP SDK will set it after validating outputMap.
	// Also, we shouldn't set Content to raw stdout when we have structured content.
	callToolResult.Content = nil
	return callToolResult, outputMap, nil
}

// Reload reloads configuration and rescans tools directory
func (o *OrlaServer) Reload() error {
	// Panic recovery for reload operation
	defer func() {
		if r := recover(); r != nil {
			core.LogPanicRecovery("reload", r)
		}
	}()

	var newCfg *state.OrlaConfig
	var err error

	if o.configPath == "" {
		newCfg, err = state.NewDefaultOrlaConfig()
	} else {
		newCfg, err = state.NewOrlaConfigFromPath(o.configPath)
	}

	if err != nil {
		return fmt.Errorf("failed to reload configuration: %w", err)
	}

	// Recreate executor in case timeout changed
	o.executor = core.NewOrlaToolExecutor(newCfg.Timeout)
	o.config = newCfg

	o.rebuildServer()
	return nil
}

// Serve starts the server on the given address using HTTP (Streamable HTTP transport per MCP spec)
// The StreamableHTTPHandler manages sessions, Origin validation, and HTTP protocol details
func (o *OrlaServer) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	// MCP endpoint that handles both POST (client requests) and GET (SSE stream)
	// StreamableHTTPHandler handles session management, Origin validation, etc.
	mux.Handle("/mcp", o.httpHandler)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	zap.L().Info("Server listening", zap.String("address", addr))

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			zap.L().Error("Server shutdown error", zap.Error(err))
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

// ServeStdio starts the server using stdio transport (per MCP spec)
func (o *OrlaServer) ServeStdio(ctx context.Context) error {
	transport := &mcp.StdioTransport{}
	// Capture the server instance with a read lock to ensure consistency.
	// Note: stdio mode typically runs once at startup, so hot reload during
	// stdio operation is unlikely, but we lock for safety.
	o.mu.RLock()
	server := o.orlaMCPserver
	o.mu.RUnlock()
	return server.Run(ctx, transport)
}
