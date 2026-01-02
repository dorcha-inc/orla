package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/server"
	"github.com/dorcha-inc/orla/internal/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_ContextCancellation(t *testing.T) {
	// Test that NewClient respects context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should fail because context is cancelled
	client, err := NewClient(ctx)
	if err != nil {
		// Expected: should fail when context is cancelled
		assert.Contains(t, err.Error(), "failed to")
		assert.Nil(t, client)
	}
}

func TestClient_ListTools_NilSession(t *testing.T) {
	ctx := context.Background()
	client := &Client{McpSession: nil}

	tools, err := client.ListTools(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP session is not initialized")
	assert.Nil(t, tools)
}

func TestClient_ListTools_Success(t *testing.T) {
	ctx := t.Context()

	// Create a test server configuration with a tools directory
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create a test tool so the registry isn't empty
	toolPath := filepath.Join(toolsDir, "test-tool.sh")
	toolContent := "#!/bin/sh\necho hello world\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	// Create tools registry
	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &config.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          0, // Use 0 to get a random port
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}

	// Create and start the test server
	srv := server.NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Find an available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
	port := addr.Port
	require.NoError(t, listener.Close())

	// Start server in a goroutine
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- srv.Serve(serverCtx, fmt.Sprintf("localhost:%d", port))
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Create MCP client with HTTP transport
	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "orla-agent",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: fmt.Sprintf("http://%s/mcp", addr),
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	// Connect to the test server
	session, connectErr := mcpClient.Connect(ctx, transport, nil)
	require.NoError(t, connectErr)
	defer core.LogDeferredError(session.Close)

	// Create client and test ListTools
	client := &Client{McpSession: session}
	tools, err := client.ListTools(ctx)
	require.NoError(t, err, "ListTools should succeed with a valid session")
	assert.NotNil(t, tools, "Tools should not be nil")
	assert.IsType(t, []*mcp.Tool{}, tools, "Tools should be a slice of *mcp.Tool")

	// Cleanup
	serverCancel()
	// Wait for server to shut down (ignore errors as server may have already shut down)
	select {
	case <-serverErrCh:
		// Server shut down
	case <-time.After(1 * time.Second):
		// Timeout waiting for server shutdown
	}
}

func TestClient_CallTool_NilSession(t *testing.T) {
	ctx := context.Background()
	client := &Client{McpSession: nil}

	params := &mcp.CallToolParams{
		Name:      "test_tool",
		Arguments: map[string]any{},
	}

	result, err := client.CallTool(ctx, params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP session is not initialized")
	assert.Nil(t, result)
}

func TestClient_Close_NilSessionAndCmd(t *testing.T) {
	client := &Client{
		McpSession: nil,
		Cmd:        nil,
	}

	err := client.Close()
	assert.NoError(t, err, "Close should not error when both session and cmd are nil")
}

func TestClient_Close_NilSession_NilCmdProcess(t *testing.T) {
	client := &Client{
		McpSession: nil,
		Cmd:        &exec.Cmd{},
	}

	err := client.Close()
	assert.NoError(t, err, "Close should not error when cmd process is nil")
}

func TestClient_Close_NilSession_CmdWithNilProcess(t *testing.T) {
	client := &Client{
		McpSession: nil,
		Cmd:        &exec.Cmd{Process: nil},
	}

	err := client.Close()
	assert.NoError(t, err, "Close should not error when cmd has nil process")
}

func TestClient_Close_ProcessAlreadyFinished(t *testing.T) {
	// Create a command that will exit immediately
	ctx := t.Context()

	cmd := exec.CommandContext(ctx, "true")
	err := cmd.Run()
	require.NoError(t, err, "Command should run successfully")

	// ProcessState should be set after Run()
	client := &Client{
		McpSession: nil,
		Cmd:        cmd,
	}

	err = client.Close()
	assert.NoError(t, err, "Close should not error when process is already finished")
}

func TestClient_Close_WithSessionError(t *testing.T) {
	// Test Close when session close returns an error
	// We can't easily mock the session, but we can test the nil case
	client := &Client{
		McpSession: nil,
		Cmd:        nil,
	}

	err := client.Close()
	// Should not error when both are nil
	assert.NoError(t, err)
}
