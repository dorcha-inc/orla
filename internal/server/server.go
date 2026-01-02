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

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/puzpuzpuz/xsync/v3"
	"go.uber.org/zap"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// OrlaServer stores the state and dependencies for the Orla MCP server.
type OrlaServer struct {
	config          *config.OrlaConfig
	configPath      string
	executor        *core.OrlaToolExecutor
	orlaMCPserver   *mcp.Server
	mu              sync.RWMutex
	httpHandler     *mcp.StreamableHTTPHandler
	capsules        *xsync.MapOf[string, *core.CapsuleManager] // the key here is the tool name
	registeredTools mapset.Set[string]                         // the key here is the tool name
}

// NewOrlaServer creates a new OrlaServer instance
func NewOrlaServer(cfg *config.OrlaConfig, configPath string) *OrlaServer {
	executor := core.NewOrlaToolExecutor(cfg.Timeout)

	orlaServer := &OrlaServer{
		config:          cfg,
		configPath:      configPath,
		executor:        executor,
		capsules:        xsync.NewMapOf[string, *core.CapsuleManager](),
		registeredTools: mapset.NewSet[string](),
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

	// Stop existing capsules before rebuilding
	o.stopAllCapsules()

	// Register each discovered tool
	for i, tool := range toolList {
		runtimeMode := core.RuntimeModeSimple
		if tool.Runtime != nil {
			runtimeMode = tool.Runtime.Mode
		}
		zap.L().Info("Registering tool with MCP server",
			zap.Int("index", i),
			zap.String("name", tool.Name),
			zap.String("path", tool.Path),
			zap.String("description", tool.Description),
			zap.String("runtime_mode", string(runtimeMode)))

		// Start capsule if tool is in capsule mode
		if runtimeMode == core.RuntimeModeCapsule {
			capsule := core.NewCapsuleManager(tool)

			startErr := capsule.Start()
			if startErr != nil {
				zap.L().Error("Failed to start capsule, skipping tool registration",
					zap.String("tool", tool.Name),
					zap.Error(startErr))
				// Skip registration if capsule fails to start
				continue
			}

			o.capsules.Store(tool.Name, capsule)
			zap.L().Info("Capsule started",
				zap.String("tool", tool.Name))
		}

		o.registerTool(tool)
	}
}

// registerTool registers a single tool with the MCP server
func (o *OrlaServer) registerTool(tool *core.ToolManifest) {
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
	if tool.MCP != nil && tool.MCP.InputSchema != nil {
		mcpTool.InputSchema = tool.MCP.InputSchema
	}

	// Add output schema if available
	if tool.MCP != nil && tool.MCP.OutputSchema != nil {
		mcpTool.OutputSchema = tool.MCP.OutputSchema
	}

	mcp.AddTool(o.orlaMCPserver, mcpTool, handler)
	zap.L().Debug("mcp.AddTool completed", zap.String("tool", tool.Name))

	o.registeredTools.Add(tool.Name)
}

// buildToolResponse builds an MCP CallToolResult and outputMap from tool execution output.
// It handles both structured (with output schema) and unstructured output.
func buildToolResponse(
	toolName string,
	stdout string,
	stderr string,
	exitCode int,
	execErr error,
	outputSchema map[string]any,
) (*mcp.CallToolResult, map[string]any) {
	// Build content from stdout
	content := []mcp.Content{
		&mcp.TextContent{
			Text: stdout,
		},
	}

	if stderr != "" {
		content = append(content, &mcp.TextContent{
			Text: fmt.Sprintf("stderr: %s", stderr),
		})
	}

	if exitCode != 0 {
		content = append(content, &mcp.TextContent{
			Text: fmt.Sprintf("exit_code: %d", exitCode),
		})
	}

	isError := execErr != nil || exitCode != 0

	callToolResult := &mcp.CallToolResult{
		IsError: isError,
		Content: content,
	}

	outputMap := make(map[string]any)

	if execErr != nil {
		outputMap["error"] = execErr.Error()
	}

	// If no output schema, wrap with default format
	if outputSchema == nil {
		outputMap["stdout"] = stdout
		outputMap["stderr"] = stderr
		outputMap["exit_code"] = exitCode
		return callToolResult, outputMap
	}

	// If tool has an output schema, try to parse stdout as JSON and use it as structured output
	var parsedOutput any
	if err := json.Unmarshal([]byte(stdout), &parsedOutput); err != nil {
		zap.L().Error("Failed to parse tool output as JSON",
			zap.String("tool", toolName),
			zap.String("stdout", stdout),
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
		}, nil
	}

	// Successfully parsed JSON. If it's a map, use it directly
	// Otherwise wrap it. Output schemas typically define objects.
	parsedMap, ok := parsedOutput.(map[string]any)

	// Note(jadidbourbaki): this is unlikely to happen, but we handle it gracefully
	if !ok {
		zap.L().Error("Tool output is not a map",
			zap.String("tool", toolName),
			zap.String("stdout", stdout))
		// If we have an OutputSchema, we can't fall back to wrapper format
		// because it won't match the schema. Return an error instead.
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: "Tool output is not a JSON object",
				},
			},
		}, nil
	}

	outputMap = parsedMap

	// Note(jadidbourbaki): After some experimentation, it seems like we
	// should not set StructuredContent manually, the MCP SDK will set it after validating outputMap.
	// Also, we shouldn't set Content to raw stdout when we have structured content.
	callToolResult.Content = nil
	return callToolResult, outputMap
}

// handleToolCall handles a tool execution request
func (o *OrlaServer) handleToolCall(
	ctx context.Context,
	tool *core.ToolManifest,
	input map[string]any,
) (*mcp.CallToolResult, map[string]any, error) {
	// Check if tool is in capsule mode
	runtimeMode := core.RuntimeModeSimple
	if tool.Runtime != nil {
		runtimeMode = tool.Runtime.Mode
	}

	// For capsule mode, communicate with the running process via JSON-RPC
	if runtimeMode == core.RuntimeModeCapsule {
		return o.handleCapsuleToolCall(ctx, tool, input)
	}

	startTime := time.Now()

	// For simple mode, execute on-demand
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

	if err != nil {
		duration := time.Since(startTime).Seconds()
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

	var outputSchema map[string]any
	if tool.MCP != nil && tool.MCP.OutputSchema != nil {
		outputSchema = tool.MCP.OutputSchema
	}

	callToolResult, outputMap := buildToolResponse(
		tool.Name,
		result.Stdout,
		result.Stderr,
		result.ExitCode,
		result.Error,
		outputSchema,
	)

	duration := time.Since(startTime).Seconds()
	core.LogToolExecution(tool.Name, duration, nil)

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

	var newCfg *config.OrlaConfig
	newCfg, err := config.LoadConfig(o.configPath)

	if err != nil {
		return fmt.Errorf("failed to reload configuration: %w", err)
	}

	// Recreate executor in case timeout changed
	o.executor = core.NewOrlaToolExecutor(newCfg.Timeout)
	o.config = newCfg

	o.rebuildServer()
	return nil
}

// stopAllCapsules stops all running capsules
func (o *OrlaServer) stopAllCapsules() {
	o.capsules.Range(func(name string, capsule *core.CapsuleManager) bool {
		if err := capsule.Stop(); err != nil {
			zap.L().Error("Failed to stop capsule",
				zap.String("tool", name),
				zap.Error(err))
		} else {
			zap.L().Debug("Stopped capsule", zap.String("tool", name))
		}
		return true
	})

	o.capsules.Clear()
}

// handleCapsuleToolCall handles tool calls for capsule mode tools by sending JSON-RPC requests to the running process
func (o *OrlaServer) handleCapsuleToolCall(
	ctx context.Context,
	tool *core.ToolManifest,
	input map[string]any,
) (*mcp.CallToolResult, map[string]any, error) {
	callStartTime := time.Now()

	// Get the capsule manager for this tool
	capsule, capsuleOk := o.capsules.Load(tool.Name)

	if !capsuleOk {
		duration := time.Since(callStartTime).Seconds()
		core.LogToolExecution(tool.Name, duration, fmt.Errorf("capsule not found: %s", tool.Name))

		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Capsule '%s' is not running", tool.Name),
				},
			},
		}, nil, fmt.Errorf("capsule not found: %s", tool.Name)
	}

	// Send JSON-RPC request to capsule
	jsonrpcResponse, callErr := capsule.CallTool(ctx, input)

	if callErr != nil {
		duration := time.Since(callStartTime).Seconds()
		core.LogToolExecution(tool.Name, duration, callErr)
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Capsule tool call failed: %v", callErr),
				},
			},
		}, nil, callErr
	}

	// Check for JSON-RPC error
	if jsonrpcResponse.Error != nil {
		duration := time.Since(callStartTime).Seconds()
		core.LogToolExecution(tool.Name, duration, fmt.Errorf("JSON-RPC error: %s", jsonrpcResponse.Error.Message))
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: jsonrpcResponse.Error.Message,
				},
			},
		}, nil, fmt.Errorf("JSON-RPC error: %s", jsonrpcResponse.Error.Message)
	}

	// Convert JSON-RPC result to stdout string
	var stdoutStr string

	if jsonrpcResponse.Result != nil {
		// If result is a string, use it directly
		if strResult, ok := jsonrpcResponse.Result.(string); ok {
			stdoutStr = strResult
		} else {
			// Otherwise, marshal to JSON string
			resultBytes, jsonErr := json.Marshal(jsonrpcResponse.Result)
			if jsonErr != nil {
				duration := time.Since(callStartTime).Seconds()
				core.LogToolExecution(tool.Name, duration, fmt.Errorf("failed to serialize capsule result: %w", jsonErr))
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("Failed to serialize capsule result: %v", jsonErr),
						},
					},
				}, nil, fmt.Errorf("failed to serialize result: %w", jsonErr)
			}
			stdoutStr = string(resultBytes)
		}
	}

	var outputSchema map[string]any
	if tool.MCP != nil && tool.MCP.OutputSchema != nil {
		outputSchema = tool.MCP.OutputSchema
	}

	// If we have an output schema and the result is already a map, use it directly
	// Otherwise, let buildToolResponse parse it from the stdout string
	if outputSchema != nil {
		resultMap, resultMapOk := jsonrpcResponse.Result.(map[string]any)
		if resultMapOk {
			// Result is already a map, use it directly
			callToolResult := &mcp.CallToolResult{
				IsError: false,
				Content: nil, // Structured content, no text content
			}

			duration := time.Since(callStartTime).Seconds()
			core.LogToolExecution(tool.Name, duration, nil)

			return callToolResult, resultMap, nil
		}
	}

	// Use the shared response builder (will parse JSON from stdout if needed)
	callToolResult, outputMap := buildToolResponse(
		tool.Name,
		stdoutStr,
		"",  // No stderr for capsule mode
		0,   // No exit code for capsule mode
		nil, // No execution error for capsule mode
		outputSchema,
	)

	duration := time.Since(callStartTime).Seconds()
	core.LogToolExecution(tool.Name, duration, nil)

	return callToolResult, outputMap, nil
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
