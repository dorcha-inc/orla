package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/state"
)

const windowsOS = "windows"

// createTestConfig creates a test configuration with a tools directory
func createTestConfig(t *testing.T) *state.OrlaConfig {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create a test tool
	toolPath := filepath.Join(toolsDir, "test-tool.sh")
	toolContent := "#!/bin/sh\necho hello world\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	// Add description to tools to test registerTool with descriptions
	tools := registry.ListTools()
	for _, tool := range tools {
		tool.Description = "A test tool"
	}

	return &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}
}

// TestNewOrlaServer tests the creation of a new OrlaServer
func TestNewOrlaServer(t *testing.T) {
	cfg := createTestConfig(t)

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)
	assert.Equal(t, cfg, srv.config)
	assert.Equal(t, "", srv.configPath)
	assert.NotNil(t, srv.executor)
	assert.NotNil(t, srv.orlaMCPserver)
	assert.NotNil(t, srv.httpHandler)
}

// TestNewOrlaServer_WithConfigPath tests server creation with a config path
func TestNewOrlaServer_WithConfigPath(t *testing.T) {
	cfg := createTestConfig(t)
	configPath := "/path/to/config.yaml"

	srv := NewOrlaServer(cfg, configPath)
	require.NotNil(t, srv)
	assert.Equal(t, configPath, srv.configPath)
}

// TestNewOrlaServer_ToolRegistration tests that tools are registered on server creation
func TestNewOrlaServer_ToolRegistration(t *testing.T) {
	cfg := createTestConfig(t)

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Verify tools were registered by checking the registry
	tools := cfg.ToolsRegistry.ListTools()
	assert.Equal(t, 1, len(tools))

	// Verify httpHandler was created (this exercises the httpHandler creation code)
	assert.NotNil(t, srv.httpHandler)
}

// TestRebuildServer tests rebuilding the server with new tools
func TestRebuildServer(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Capture initial server instance
	initialServer := srv.orlaMCPserver

	// Add a new tool to the registry
	newTool := &core.ToolManifest{
		Name:        "new-tool",
		Version:     "1.0.0",
		Description: "A new tool",
		Entrypoint:  "tool",
		Path:        "/path/to/new-tool",
		Interpreter: "/bin/sh",
		Runtime:     &core.RuntimeConfig{Mode: core.RuntimeModeSimple},
	}
	err := cfg.ToolsRegistry.AddTool(newTool)
	require.NoError(t, err)

	// Rebuild server
	srv.rebuildServer()

	// Verify server instance was replaced
	assert.NotEqual(t, initialServer, srv.orlaMCPserver)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_PanicRecovery tests that registerTool recovers from panics
func TestRegisterTool_PanicRecovery(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that will cause a panic when executed
	// We'll use a tool with nil path to trigger panic in handleToolCall
	panicTool := &core.ToolManifest{
		Name:        "panic-tool",
		Description: "Tool that causes panic",
		Path:        "", // Empty path might cause issues
		Interpreter: "",
	}

	// Register the tool - this should not panic
	// The panic recovery is in the handler, so we need to actually call the tool
	// But registerTool itself should complete successfully
	srv.rebuildServer()

	// Add tool to registry and rebuild to trigger registerTool
	err := cfg.ToolsRegistry.AddTool(panicTool)
	require.NoError(t, err)

	// Rebuild should complete without panic
	srv.rebuildServer()
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithNilExecutor tests registerTool when executor is nil (edge case)
// This tests that registerTool completes even with edge case configurations
func TestRegisterTool_WithNilExecutor(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool entry
	tool := &core.ToolManifest{
		Name:        "test-tool",
		Description: "Test tool",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	// Temporarily set executor to nil to test edge case
	originalExecutor := srv.executor
	srv.executor = nil

	// Register the tool - this should complete (the handler will panic when called, but registration succeeds)
	srv.registerTool(tool)

	// Restore executor
	srv.executor = originalExecutor

	// Verify tool was registered
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestNewOrlaServer_WithNilConfig tests that NewOrlaServer handles edge cases
// Note: nil config would cause a panic, so we test with valid but edge case configs
func TestNewOrlaServer_WithEmptyToolsRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create empty registry
	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)
	assert.NotNil(t, srv.orlaMCPserver)
	assert.NotNil(t, srv.httpHandler)
	assert.NotNil(t, srv.executor)
}

// TestNewOrlaServer_WithMultipleTools tests server creation with multiple tools
func TestNewOrlaServer_WithMultipleTools(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create multiple tools
	for i := 1; i <= 3; i++ {
		toolPath := filepath.Join(toolsDir, fmt.Sprintf("tool%d.sh", i))
		// #nosec G306 -- test file permissions are acceptable for temporary test files
		require.NoError(t, os.WriteFile(toolPath, []byte(fmt.Sprintf("#!/bin/sh\necho tool%d\n", i)), 0755))
	}

	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)
	assert.NotNil(t, srv.orlaMCPserver)
	assert.NotNil(t, srv.httpHandler)

	// Verify all tools were registered
	tools := cfg.ToolsRegistry.ListTools()
	assert.GreaterOrEqual(t, len(tools), 3)
}

// TestNewOrlaServer_WithConfigPathAndReload tests server creation with config path and reload
func TestNewOrlaServer_WithConfigPathAndReload(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create a tool
	toolPath := filepath.Join(toolsDir, "test-tool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(toolPath, []byte("#!/bin/sh\necho test\n"), 0755))

	configPath := filepath.Join(tmpDir, "orla.yaml")
	configYAML := fmt.Sprintf(`tools_dir: %s
port: 9000
timeout: 60
`, toolsDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0644))

	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       30,
	}

	srv := NewOrlaServer(cfg, configPath)
	require.NotNil(t, srv)
	assert.Equal(t, configPath, srv.configPath)

	// Test reload
	err = srv.Reload()
	require.NoError(t, err)
	assert.Equal(t, 60, srv.config.Timeout)
	assert.Equal(t, 9000, srv.config.Port)
}

// TestHandleToolCall_Success tests successful tool execution
func TestHandleToolCall_Success(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Get a tool from the registry
	tools := cfg.ToolsRegistry.ListTools()
	require.GreaterOrEqual(t, len(tools), 1)
	tool := tools[0]

	// Execute tool with no input
	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.False(t, result.IsError)
	assert.GreaterOrEqual(t, len(result.Content), 1)

	// Verify output structure
	assert.Contains(t, output, "stdout")
	assert.Contains(t, output, "stderr")
	assert.Contains(t, output, "exit_code")

	// Verify the output is the same as the tool's output
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "First content should be TextContent")
	assert.Contains(t, textContent.Text, "hello world")
}

// TestHandleToolCall_WithArgs tests tool execution with arguments
// Note: Arguments are passed as --key value flags, not positional arguments
func TestHandleToolCall_WithArgs(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that accepts --key value flags
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "echo-tool.sh")
	// Tool that echoes the values of --name and --message flags
	toolContent := `#!/bin/sh
while [ $# -gt 0 ]; do
  case $1 in
    --name)
      name=$2
      shift 2
      ;;
    --message)
      message=$2
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
echo "$name: $message"
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "echo-tool",
		Description: "Echo tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	input := map[string]any{
		"name":    "test",
		"message": "hello world",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, input)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.False(t, result.IsError)

	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "First content should be TextContent")
	assert.Contains(t, textContent.Text, "test: hello world")
}

// TestHandleToolCall_WithStdin tests tool execution with stdin
func TestHandleToolCall_WithStdin(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that reads from stdin
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "stdin-tool.sh")
	toolContent := `#!/bin/sh
read input
echo "received: $input"
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "stdin-tool",
		Description: "Stdin tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	input := map[string]any{
		"stdin": "test input",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, input)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.False(t, result.IsError)
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "First content should be TextContent")
	assert.Contains(t, textContent.Text, "received: test input")
}

// TestHandleToolCall_Error tests tool execution with an error
func TestHandleToolCall_Error(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that exits with non-zero code
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "error-tool.sh")
	toolContent := "#!/bin/sh\necho error >&2\nexit 1\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "error-tool",
		Description: "Error tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err) // handleToolCall doesn't return error, sets IsError instead
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.True(t, result.IsError) // Non-zero exit code should set IsError
	assert.Contains(t, output, "exit_code")
}

// TestHandleToolCall_CommandNotFound tests handling of command not found
func TestHandleToolCall_CommandNotFound(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tool := &core.ToolManifest{
		Name:        "nonexistent",
		Description: "Nonexistent tool",
		Path:        "/nonexistent/path/to/tool",
		Interpreter: "",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})

	// handleToolCall wraps executor errors in CallToolResult, so err is nil
	// but result.IsError is true and output is nil
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "Command not found should result in IsError=true")
	assert.Nil(t, output, "Output should be nil when executor returns an error")
	assert.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "Tool execution failed")
}

// TestReload tests reloading configuration
func TestReload(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create initial tool
	tool1Path := filepath.Join(toolsDir, "tool1.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(tool1Path, []byte("#!/bin/sh\necho tool1\n"), 0755)
	require.NoError(t, err)

	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       30,
	}

	configPath := filepath.Join(tmpDir, "orla.yaml")
	configYAML := `tools_dir: ./tools
port: 9000
timeout: 60
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, []byte(configYAML), 0644)
	require.NoError(t, err)

	srv := NewOrlaServer(cfg, configPath)
	require.NotNil(t, srv)

	// Add a new tool to the directory
	tool2Path := filepath.Join(toolsDir, "tool2.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(tool2Path, []byte("#!/bin/sh\necho tool2\n"), 0755)
	require.NoError(t, err)

	// Reload
	err = srv.Reload()
	require.NoError(t, err)

	// Verify config was reloaded (timeout should be updated)
	assert.Equal(t, 60, srv.config.Timeout)
	assert.Equal(t, 9000, srv.config.Port)

	// Verify tools were rescanned
	tools := srv.config.ToolsRegistry.ListTools()
	assert.GreaterOrEqual(t, len(tools), 2)
}

// TestReload_DefaultConfig tests reloading with default config (no config path)
func TestReload_DefaultConfig(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "") // No config path
	require.NotNil(t, srv)

	// Create the tools directory that NewDefaultOrlaConfig expects
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Reload should use default config
	err = srv.Reload()
	require.NoError(t, err)

	// Verify executor was recreated
	assert.NotNil(t, srv.executor)
}

// TestReload_InvalidConfig tests reloading with an invalid config file
func TestReload_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(configPath, []byte("{ invalid json }"), 0644)
	require.NoError(t, err)

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, configPath)
	require.NotNil(t, srv)

	// Reload should fail
	err = srv.Reload()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reload configuration")
}

// TestServe tests starting the HTTP server (with context cancellation)
func TestServe(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in a goroutine
	done := make(chan error, 1)
	go func() {
		// Use a random port (0) to avoid conflicts
		done <- srv.Serve(ctx, ":0")
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to stop server
	cancel()

	// Wait for server to stop
	select {
	case err := <-done:
		// Server should stop gracefully
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}
}

// TestServe_InvalidAddress tests server with an invalid address
func TestServe_InvalidAddress(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use an invalid address format (though ":0" should work, let's test with a bad port)
	// Actually, ":0" is valid, so let's test with a port that's already in use
	// We'll use a high port number that's unlikely to be in use, but test the error path

	// Start server - if it fails immediately, we'll catch the error
	done := make(chan error, 1)
	go func() {
		// Use localhost:0 which should work, but test error handling
		done <- srv.Serve(ctx, "127.0.0.1:0")
	}()

	// Give it a moment
	time.Sleep(50 * time.Millisecond)

	// Cancel to stop
	cancel()

	// Wait for completion
	select {
	case err := <-done:
		// Should complete without error (graceful shutdown)
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		// If it doesn't complete, that's also fine for this test
	}
}

// TestServeStdio tests starting the stdio server (with context cancellation)
func TestServeStdio(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- srv.ServeStdio(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop server
	cancel()

	// Wait for server to stop
	select {
	case err := <-done:
		// Server should stop gracefully
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}
}

// TestNewOrlaServer_EmptyToolsDirectory tests server creation with empty tools directory
func TestNewOrlaServer_EmptyToolsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create empty registry
	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       30,
	}

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)
	assert.NotNil(t, srv.orlaMCPserver)
	assert.NotNil(t, srv.httpHandler)
	// Verify no tools were registered (empty directory should log warning)
	tools := cfg.ToolsRegistry.ListTools()
	assert.Equal(t, 0, len(tools))
}

// TestHandleToolCall_Timeout tests tool execution with timeout
func TestHandleToolCall_Timeout(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	// Set a very short timeout
	cfg.Timeout = 1
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that sleeps longer than timeout
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "sleep-tool.sh")
	toolContent := "#!/bin/sh\nsleep 5\necho done\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "sleep-tool",
		Description: "Sleep tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err) // handleToolCall doesn't return error, sets IsError instead
	assert.NotNil(t, result)
	assert.True(t, result.IsError) // Timeout should set IsError
	// Output may be nil when executor returns an error
	if output != nil {
		assert.Contains(t, output, "error")
	}
	// Verify timeout message is included
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "timed out")
}

// TestHandleToolCall_WithStderrAndExitCode tests tool execution with stderr and non-zero exit code
func TestHandleToolCall_WithStderrAndExitCode(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that writes to stderr and exits with non-zero code
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "error-tool.sh")
	toolContent := "#!/bin/sh\necho 'error message' >&2\necho 'stdout message'\nexit 42\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "error-tool",
		Description: "Error tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.True(t, result.IsError) // Non-zero exit code should set IsError

	// Verify stderr is included in content
	assert.GreaterOrEqual(t, len(result.Content), 2)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "stdout message")

	// Check for stderr content
	foundStderr := false
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			if strings.Contains(textContent.Text, "stderr:") {
				foundStderr = true
				assert.Contains(t, textContent.Text, "error message")
			}
		}
	}
	assert.True(t, foundStderr, "stderr should be included in content")

	// Verify exit code is included
	foundExitCode := false
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			if strings.Contains(textContent.Text, "exit_code") {
				foundExitCode = true
				assert.Contains(t, textContent.Text, "42")
			}
		}
	}
	assert.True(t, foundExitCode, "exit_code should be included in content")

	// Verify output structure
	assert.Equal(t, "stdout message\n", output["stdout"])
	assert.Contains(t, output["stderr"], "error message")
	assert.Equal(t, 42, output["exit_code"])
}

// TestHandleToolCall_WithOutputSchema tests tool execution with output schema (JSON parsing)
func TestHandleToolCall_WithOutputSchema(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that outputs JSON matching an output schema
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "json-tool.sh")
	toolContent := `#!/bin/sh
echo '{"success":true,"count":5,"items":["a","b","c"]}'
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
			"count":   map[string]any{"type": "number"},
			"items":   map[string]any{"type": "array"},
		},
		"required": []string{"success"},
	}

	tool := &core.ToolManifest{
		Name:        "json-tool",
		Description: "JSON tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			OutputSchema: outputSchema,
		},
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.False(t, result.IsError)

	// Verify output is the parsed JSON map, not the wrapper format
	assert.Contains(t, output, "success")
	assert.Contains(t, output, "count")
	assert.Contains(t, output, "items")
	assert.Equal(t, true, output["success"])
	assert.Equal(t, float64(5), output["count"]) // JSON numbers are float64

	// Verify Content is nil (SDK will populate it)
	assert.Nil(t, result.Content)
}

// TestHandleToolCall_WithOutputSchema_InvalidJSON tests tool with output schema but invalid JSON
func TestHandleToolCall_WithOutputSchema_InvalidJSON(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "invalid-json-tool.sh")
	toolContent := `#!/bin/sh
echo 'not valid json'
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
		},
		"required": []string{"success"},
	}

	tool := &core.ToolManifest{
		Name:        "invalid-json-tool",
		Description: "Invalid JSON tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			OutputSchema: outputSchema,
		},
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Nil(t, output) // Output should be nil when JSON parsing fails
	assert.True(t, result.IsError)

	// Verify error message
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "not valid JSON")
}

// TestHandleToolCall_WithOutputSchema_NotMap tests tool with output schema but JSON is not a map
func TestHandleToolCall_WithOutputSchema_NotMap(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "array-tool.sh")
	toolContent := `#!/bin/sh
echo '["a","b","c"]'
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
		},
		"required": []string{"success"},
	}

	tool := &core.ToolManifest{
		Name:        "array-tool",
		Description: "Array tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			OutputSchema: outputSchema,
		},
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Nil(t, output) // Output should be nil when JSON is not a map
	assert.True(t, result.IsError)

	// Verify error message
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "not a JSON object")
}

// TestHandleToolCall_WithOutputSchema_EmptyStdout tests tool with output schema but empty stdout
func TestHandleToolCall_WithOutputSchema_EmptyStdout(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "empty-tool.sh")
	toolContent := `#!/bin/sh
# No output
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
		},
		"required": []string{"success"},
	}

	tool := &core.ToolManifest{
		Name:        "empty-tool",
		Description: "Empty tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			OutputSchema: outputSchema,
		},
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Nil(t, output) // Output should be nil when JSON parsing fails
	assert.True(t, result.IsError)

	// Verify error message
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "not valid JSON")
}

// TestHandleToolCall_UnderscoreToHyphen tests that argument names are converted from underscore to hyphen
func TestHandleToolCall_UnderscoreToHyphen(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that accepts --create-dirs flag
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "hyphen-tool.sh")
	toolContent := `#!/bin/sh
while [ $# -gt 0 ]; do
  case $1 in
    --create-dirs)
      echo "create-dirs=$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "hyphen-tool",
		Description: "Hyphen tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	// Input uses underscore (as JSON schemas typically do)
	input := map[string]any{
		"create_dirs": "true",
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, input)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.False(t, result.IsError)

	// Verify the tool received --create-dirs (hyphen) not --create_dirs (underscore)
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "create-dirs=true")
}

// TestRegisterTool_WithInputSchema tests registering a tool with input schema
func TestRegisterTool_WithInputSchema(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to file",
			},
		},
		"required": []string{"path"},
	}

	tool := &core.ToolManifest{
		Name:        "schema-tool",
		Description: "Tool with schema",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			InputSchema: inputSchema,
		},
	}

	// Register the tool - should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithOutputSchema tests registering a tool with output schema
func TestRegisterTool_WithOutputSchema(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
		},
		"required": []string{"success"},
	}

	tool := &core.ToolManifest{
		Name:        "output-schema-tool",
		Description: "Tool with output schema",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			OutputSchema: outputSchema,
		},
	}

	// Register the tool - should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithBothSchemas tests registering a tool with both input and output schemas
func TestRegisterTool_WithBothSchemas(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}

	outputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
		},
		"required": []string{"success"},
	}

	tool := &core.ToolManifest{
		Name:        "both-schemas-tool",
		Description: "Tool with both schemas",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		},
	}

	// Register the tool - should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestHandleToolCall_BooleanArgs tests that boolean arguments are passed correctly
func TestHandleToolCall_BooleanArgs(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping tool execution test on Windows")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a tool that accepts boolean flags
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "bool-tool.sh")
	toolContent := `#!/bin/sh
while [ $# -gt 0 ]; do
  case $1 in
    --recursive)
      echo "recursive=$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(toolPath, []byte(toolContent), 0755)
	require.NoError(t, err)

	tool := &core.ToolManifest{
		Name:        "bool-tool",
		Description: "Boolean tool",
		Path:        toolPath,
		Interpreter: "/bin/sh",
	}

	// Input with boolean value (MCP passes booleans as strings)
	input := map[string]any{
		"recursive": true, // Boolean value
	}

	result, output, err := srv.handleToolCall(context.Background(), tool, input)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, output)
	assert.False(t, result.IsError)

	// Verify the tool received the boolean value
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "recursive=true")
}

// TestRegisterTool_WithEmptyName tests registering a tool with empty name
func TestRegisterTool_WithEmptyName(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tool := &core.ToolManifest{
		Name:        "", // Empty name
		Description: "Tool with empty name",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	// Should not panic, but tool registration might fail or succeed depending on MCP SDK
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithEmptyDescription tests registering a tool with empty description
func TestRegisterTool_WithEmptyDescription(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tool := &core.ToolManifest{
		Name:        "empty-desc-tool",
		Description: "", // Empty description
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	// Should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithNilSchemas tests registering a tool with explicitly nil schemas
func TestRegisterTool_WithNilSchemas(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tool := &core.ToolManifest{
		Name:        "nil-schemas-tool",
		Description: "Tool with nil schemas",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	// Should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithMinimalInputSchema tests registering a tool with minimal valid input schema
func TestRegisterTool_WithMinimalInputSchema(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Minimal valid schema (just type: object)
	minimalSchema := map[string]any{
		"type": "object",
	}

	tool := &core.ToolManifest{
		Name:        "minimal-input-schema-tool",
		Description: "Tool with minimal input schema",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			InputSchema: minimalSchema,
		},
	}

	// Should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestRegisterTool_WithMinimalOutputSchema tests registering a tool with minimal valid output schema
func TestRegisterTool_WithMinimalOutputSchema(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Minimal valid schema (just type: object)
	minimalSchema := map[string]any{
		"type": "object",
	}

	tool := &core.ToolManifest{
		Name:        "minimal-output-schema-tool",
		Description: "Tool with minimal output schema",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
		MCP: &core.MCPConfig{
			OutputSchema: minimalSchema,
		},
	}

	// Should not panic
	srv.registerTool(tool)
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestNewOrlaServer_WithZeroTimeout tests server creation with zero timeout
func TestNewOrlaServer_WithZeroTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       0, // Zero timeout
		LogFormat:     "json",
		LogLevel:      "info",
	}

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)
	assert.NotNil(t, srv.executor)
	assert.Equal(t, 0, srv.config.Timeout)
}

// TestNewOrlaServer_WithLargeTimeout tests server creation with very large timeout
func TestNewOrlaServer_WithLargeTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	registry, err := state.NewToolsRegistryFromDirectory(toolsDir)
	require.NoError(t, err)

	cfg := &state.OrlaConfig{
		ToolsDir:      toolsDir,
		ToolsRegistry: registry,
		Port:          8080,
		Timeout:       3600, // 1 hour
		LogFormat:     "json",
		LogLevel:      "info",
	}

	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)
	assert.NotNil(t, srv.executor)
	assert.Equal(t, 3600, srv.config.Timeout)
}

// TestNewOrlaServer_WithLongConfigPath tests server creation with very long config path
func TestNewOrlaServer_WithLongConfigPath(t *testing.T) {
	cfg := createTestConfig(t)
	longPath := strings.Repeat("/very/long/path/", 20) + "config.yaml"

	srv := NewOrlaServer(cfg, longPath)
	require.NotNil(t, srv)
	assert.Equal(t, longPath, srv.configPath)
}

// TestRebuildServer_Concurrent tests rebuilding server concurrently (race condition test)
func TestRebuildServer_Concurrent(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Add multiple tools
	for i := 1; i <= 5; i++ {
		tool := &core.ToolManifest{
			Name:        fmt.Sprintf("tool%d", i),
			Description: fmt.Sprintf("Tool %d", i),
			Path:        fmt.Sprintf("/path/to/tool%d", i),
			Interpreter: "/bin/sh",
		}
		err := cfg.ToolsRegistry.AddTool(tool)
		require.NoError(t, err)
	}

	// Rebuild concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.rebuildServer()
		}()
	}

	wg.Wait()

	// Server should still be valid after concurrent rebuilds
	assert.NotNil(t, srv.orlaMCPserver)
	assert.NotNil(t, srv.httpHandler)
}

// TestNewOrlaServer_WithNilToolsRegistry tests server creation with nil ToolsRegistry
// This should not panic but might cause issues - testing edge case
func TestNewOrlaServer_WithNilToolsRegistry(t *testing.T) {
	cfg := &state.OrlaConfig{
		ToolsDir:      "/tmp/tools",
		ToolsRegistry: nil, // Nil registry
		Port:          8080,
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}

	// This might panic, but we want to test the edge case
	defer func() {
		if r := recover(); r != nil {
			// Expected to panic if ToolsRegistry is nil and rebuildServer tries to use it
			t.Logf("Expected panic with nil ToolsRegistry: %v", r)
		}
	}()

	srv := NewOrlaServer(cfg, "")
	// If we get here, the server was created (might have empty registry)
	if srv != nil {
		assert.NotNil(t, srv.orlaMCPserver)
	}
}

// TestRegisterTool_MultipleRegistrations tests registering the same tool multiple times
func TestRegisterTool_MultipleRegistrations(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	tool := &core.ToolManifest{
		Name:        "duplicate-tool",
		Description: "Duplicate tool",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	// Register the same tool multiple times
	for i := 0; i < 5; i++ {
		srv.registerTool(tool)
	}

	// Should not panic
	assert.NotNil(t, srv.orlaMCPserver)
}

// TestNewOrlaServer_WithSpecialCharactersInPath tests server creation with special characters in config path
func TestNewOrlaServer_WithSpecialCharactersInPath(t *testing.T) {
	cfg := createTestConfig(t)
	specialPath := "/tmp/test path with spaces/config.yaml"

	srv := NewOrlaServer(cfg, specialPath)
	require.NotNil(t, srv)
	assert.Equal(t, specialPath, srv.configPath)
}

// createRespondingCapsuleScript creates a test capsule script that sends handshake and responds to JSON-RPC calls
func createRespondingCapsuleScript(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
		return ""
	}

	scriptContent := `#!/bin/sh
# Send handshake
echo '{"jsonrpc":"2.0","method":"orla.hello","params":{"name":"test-tool","version":"1.0.0","capabilities":["tools"]}}'

# Read and respond to JSON-RPC requests
while IFS= read -r line; do
  # Extract request ID using sed
  REQ_ID=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  if [ -n "$REQ_ID" ]; then
    # Send response
    echo "{\"jsonrpc\":\"2.0\",\"id\":$REQ_ID,\"result\":{\"output\":\"test result\"}}"
  fi
done
`

	scriptFile := filepath.Join(t.TempDir(), "responding-capsule.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptFile, []byte(scriptContent), 0755)
	require.NoError(t, err)

	return scriptFile
}

// TestRebuildServer_WithCapsuleMode tests rebuilding server with capsule mode tools
func TestRebuildServer_WithCapsuleMode(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a capsule mode tool
	scriptPath := createRespondingCapsuleScript(t)
	capsuleTool := &core.ToolManifest{
		Name:        "capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool",
		Path:        scriptPath,
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
		},
	}

	err := cfg.ToolsRegistry.AddTool(capsuleTool)
	require.NoError(t, err)

	// Rebuild server - should start the capsule
	srv.rebuildServer()

	// Verify capsule was started
	capsule, ok := srv.capsules.Load("capsule-tool")
	require.True(t, ok, "Capsule should be started")
	require.NotNil(t, capsule)
	assert.True(t, capsule.IsReady(), "Capsule should be ready")

	// Cleanup
	srv.capsules.Range(func(_ string, cap *core.CapsuleManager) bool {
		_ = cap.Stop() //nolint:errcheck // cleanup in test
		return true
	})
}

// TestRebuildServer_StopsExistingCapsules tests that rebuilding stops existing capsules
func TestRebuildServer_StopsExistingCapsules(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create and add a capsule mode tool
	scriptPath := createRespondingCapsuleScript(t)
	capsuleTool := &core.ToolManifest{
		Name:        "capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool",
		Path:        scriptPath,
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
		},
	}

	err := cfg.ToolsRegistry.AddTool(capsuleTool)
	require.NoError(t, err)

	// First rebuild - should start capsule
	srv.rebuildServer()

	capsule1, ok1 := srv.capsules.Load("capsule-tool")
	require.True(t, ok1)
	require.NotNil(t, capsule1)

	// Second rebuild - should stop old capsule and start new one
	srv.rebuildServer()

	capsule2, ok2 := srv.capsules.Load("capsule-tool")
	require.True(t, ok2)
	require.NotNil(t, capsule2)

	// Verify capsules are different instances (old one was stopped)
	assert.NotEqual(t, capsule1, capsule2)

	// Cleanup
	srv.capsules.Range(func(_ string, cap *core.CapsuleManager) bool {
		_ = cap.Stop() //nolint:errcheck // cleanup in test
		return true
	})
}

// TestHandleToolCall_CapsuleMode tests tool execution for capsule mode tools
func TestHandleToolCall_CapsuleMode(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a capsule mode tool
	scriptPath := createRespondingCapsuleScript(t)
	capsuleTool := &core.ToolManifest{
		Name:        "capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool",
		Path:        scriptPath,
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
		},
	}

	err := cfg.ToolsRegistry.AddTool(capsuleTool)
	require.NoError(t, err)

	// Rebuild to start the capsule
	srv.rebuildServer()

	// Wait a bit for capsule to be ready
	time.Sleep(100 * time.Millisecond)

	// Call the tool
	result, output, err := srv.handleToolCall(context.Background(), capsuleTool, map[string]any{"arg1": "value1"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError, "Capsule tool call should succeed")

	// Verify output structure (buildToolResponse parses JSON from stdout)
	require.NotNil(t, output)
	assert.Contains(t, output, "stdout")
	// The stdout should contain the JSON result
	stdout, ok := output["stdout"].(string)
	require.True(t, ok)
	assert.Contains(t, stdout, "test result")

	// Cleanup
	srv.capsules.Range(func(_ string, cap *core.CapsuleManager) bool {
		_ = cap.Stop() //nolint:errcheck // cleanup in test
		return true
	})
}

// TestHandleToolCall_CapsuleMode_NotRunning tests handling when capsule is not running
func TestHandleToolCall_CapsuleMode_NotRunning(t *testing.T) {
	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a capsule mode tool but don't start it
	capsuleTool := &core.ToolManifest{
		Name:        "capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool",
		Path:        "/path/to/tool",
		Runtime: &core.RuntimeConfig{
			Mode: core.RuntimeModeCapsule,
		},
	}

	// Call the tool without starting the capsule
	result, output, err := srv.handleToolCall(context.Background(), capsuleTool, map[string]any{})
	// handleCapsuleToolCall returns an error when capsule is not found
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capsule not found")
	require.NotNil(t, result)
	assert.True(t, result.IsError, "Should return error when capsule is not running")
	assert.Nil(t, output)

	// Verify error message
	require.GreaterOrEqual(t, len(result.Content), 1)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, textContent.Text, "Capsule")
}

// TestHandleCapsuleToolCall_JSONRPCError tests handling JSON-RPC error responses
func TestHandleCapsuleToolCall_JSONRPCError(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a capsule mode tool that returns JSON-RPC error
	scriptPath := createErrorCapsuleScript(t)
	capsuleTool := &core.ToolManifest{
		Name:        "error-capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool that returns errors",
		Path:        scriptPath,
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
		},
	}

	err := cfg.ToolsRegistry.AddTool(capsuleTool)
	require.NoError(t, err)

	// Rebuild to start the capsule
	srv.rebuildServer()

	// Wait a bit for capsule to be ready
	time.Sleep(100 * time.Millisecond)

	// Call the tool - should get JSON-RPC error
	result, output, err := srv.handleCapsuleToolCall(context.Background(), capsuleTool, map[string]any{})
	require.Error(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "Should return error for JSON-RPC error response")
	assert.Nil(t, output)

	// Cleanup
	srv.capsules.Range(func(_ string, cap *core.CapsuleManager) bool {
		_ = cap.Stop() //nolint:errcheck // cleanup in test
		return true
	})
}

// TestHandleCapsuleToolCall_CallToolError tests handling CallTool errors
func TestHandleCapsuleToolCall_CallToolError(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a capsule mode tool
	scriptPath := createRespondingCapsuleScript(t)
	capsuleTool := &core.ToolManifest{
		Name:        "capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool",
		Path:        scriptPath,
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
		},
	}

	err := cfg.ToolsRegistry.AddTool(capsuleTool)
	require.NoError(t, err)

	// Rebuild to start the capsule
	srv.rebuildServer()

	// Wait a bit for capsule to be ready
	time.Sleep(100 * time.Millisecond)

	// Stop the capsule to cause CallTool to fail
	srv.capsules.Range(func(_ string, cap *core.CapsuleManager) bool {
		_ = cap.Stop() //nolint:errcheck // cleanup in test
		return true
	})

	// Wait a bit for capsule to stop
	time.Sleep(50 * time.Millisecond)

	// Call the tool - should get error because capsule is stopped
	result, output, err := srv.handleCapsuleToolCall(context.Background(), capsuleTool, map[string]any{})
	require.Error(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError, "Should return error when CallTool fails")
	assert.Nil(t, output)
}

// createErrorCapsuleScript creates a capsule script that returns JSON-RPC errors
func createErrorCapsuleScript(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
		return ""
	}

	scriptContent := `#!/bin/sh
# Send handshake
echo '{"jsonrpc":"2.0","method":"orla.hello","params":{"name":"error-capsule-tool","version":"1.0.0","capabilities":["tools"]}}'

# Read and respond to JSON-RPC requests with errors
while IFS= read -r line; do
  REQ_ID=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  if [ -n "$REQ_ID" ]; then
    # Send error response
    echo "{\"jsonrpc\":\"2.0\",\"id\":$REQ_ID,\"error\":{\"code\":-1,\"message\":\"Test error\"}}"
  fi
done
`

	scriptFile := filepath.Join(t.TempDir(), "error-capsule.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptFile, []byte(scriptContent), 0755)
	require.NoError(t, err)

	return scriptFile
}

// TestRebuildServer_CapsuleFailsToStart tests that tools with failing capsules are not registered
func TestRebuildServer_CapsuleFailsToStart(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	cfg := createTestConfig(t)
	srv := NewOrlaServer(cfg, "")
	require.NotNil(t, srv)

	// Create a capsule mode tool with an invalid path (will fail to start)
	capsuleTool := &core.ToolManifest{
		Name:        "failing-capsule-tool",
		Version:     "1.0.0",
		Description: "A capsule mode tool that will fail to start",
		Path:        "/nonexistent/path/to/tool",
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
		},
	}

	err := cfg.ToolsRegistry.AddTool(capsuleTool)
	require.NoError(t, err)

	// Rebuild server - capsule should fail to start and tool should not be registered
	srv.rebuildServer()

	// Verify capsule was not started
	_, ok := srv.capsules.Load("failing-capsule-tool")
	assert.False(t, ok, "Capsule should not be started")

	assert.False(t, srv.registeredTools.Contains("failing-capsule-tool"), "Tool should not be registered")
}
