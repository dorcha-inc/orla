// Package agent implements the agent loop and MCP client for Orla Agent Mode (RFC 4).
package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// Client wraps an MCP client session for communicating with the internal Orla server
type Client struct {
	McpSession *mcp.ClientSession
	McpClient  *mcp.Client
	Cmd        *exec.Cmd
}

func getOrlaBin() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}
	orlaBin, symlinkErr := filepath.EvalSymlinks(exe)
	if symlinkErr != nil {
		return "", fmt.Errorf("failed to evaluate symlinks: %w", symlinkErr)
	}
	return orlaBin, nil
}

// NewClient creates a new MCP client that connects to the internal Orla server via stdio
// The server is started as a subprocess running "orla serve --stdio"
// If orlaBin is empty, it will use the current executable
func NewClient(ctx context.Context) (*Client, error) {
	orlaBin, binErr := getOrlaBin()
	if binErr != nil {
		return nil, fmt.Errorf("failed to get orla binary path: %w", binErr)
	}

	// Create MCP client
	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "orla-agent",
		Version: "1.0.0",
	}, nil)

	// Create stdio transport (spawns orla process)
	cmd := exec.CommandContext(ctx, orlaBin, "serve", "--stdio")
	transport := &mcp.CommandTransport{
		Command: cmd,
	}

	// Connect to server
	session, connectErr := mcpClient.Connect(ctx, transport, nil)
	if connectErr != nil {
		return nil, fmt.Errorf("failed to connect to internal MCP server: %w", connectErr)
	}

	zap.L().Debug("Connected to internal MCP server", zap.String("orla_bin", orlaBin))

	return &Client{
		McpSession: session,
		McpClient:  mcpClient,
		Cmd:        cmd,
	}, nil
}

// ListTools lists all available tools from the MCP server
func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if c.McpSession == nil {
		return nil, fmt.Errorf("MCP session is not initialized")
	}
	result, err := c.McpSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// CallTool calls a tool via the MCP server
func (c *Client) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if c.McpSession == nil {
		return nil, fmt.Errorf("MCP session is not initialized")
	}
	return c.McpSession.CallTool(ctx, params)
}

// Close closes the MCP client session and cleans up the subprocess
func (c *Client) Close() error {
	var errs []error
	if c.McpSession != nil {
		if err := c.McpSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close session: %w", err))
		}
	}

	if c.Cmd != nil && c.Cmd.Process != nil {
		// The process should be cleaned up when the context is cancelled,
		// but we'll try to kill it if it's still running
		if c.Cmd.ProcessState == nil {
			// if the process state is nil, the process is still running
			if err := c.Cmd.Process.Kill(); err != nil {
				errs = append(errs, fmt.Errorf("failed to kill subprocess: %w", err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing client: %v", errs)
	}

	return nil
}
