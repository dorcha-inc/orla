package core

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCapsuleManager(t *testing.T) {
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)
	require.NotNil(t, cm)
	assert.Equal(t, tool, cm.tool)
	assert.Equal(t, CapsuleStateCreated, cm.GetState())
	assert.Equal(t, DefaultCapsuleStartupTimeoutMs*time.Millisecond, cm.startupTimeout)
	assert.NotNil(t, cm.handshakeCh)
	assert.NotNil(t, cm.ctx)
	assert.NotNil(t, cm.cancel)
	assert.NotNil(t, cm.responses)
}

func TestNewCapsuleManager_WithCustomTimeout(t *testing.T) {
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        "/path/to/tool",
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 10000,
		},
	}

	cm := NewCapsuleManager(tool)
	require.NotNil(t, cm)
	assert.Equal(t, 10*time.Second, cm.startupTimeout)
}

func TestCapsuleManager_GetState(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)
	assert.Equal(t, CapsuleStateCreated, cm.GetState())
}

func TestCapsuleManager_IsReady(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)
	assert.False(t, cm.IsReady())

	// Manually set state to READY (for testing)
	cm.stateMu.Lock()
	cm.state = CapsuleStateReady
	cm.stateMu.Unlock()

	assert.True(t, cm.IsReady())
}

func TestCapsuleManager_Start_InvalidState(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	// Set state to READY (invalid for starting)
	cm.stateMu.Lock()
	cm.state = CapsuleStateReady
	cm.stateMu.Unlock()

	err := cm.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start capsule in state")
}

func TestCapsuleManager_Start_HandshakeTimeout(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	fakeClock := clockwork.NewFakeClock()
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/bin/sleep",
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 100, // Very short timeout
		},
	}

	cm := NewCapsuleManagerWithClock(tool, fakeClock)

	// Start a process that won't send handshake in a goroutine
	done := make(chan bool)
	var startErr error
	go func() {
		startErr = cm.Start()
		done <- true
	}()

	// Wait for the capsule to be waiting on the clock
	blockCtx, blockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer blockCancel()
	err := fakeClock.BlockUntilContext(blockCtx, 1)
	require.NoError(t, err, "Failed to block until clock has waiters")

	// Advance the fake clock past the timeout
	fakeClock.Advance(200 * time.Millisecond)

	// Wait for Start to complete
	select {
	case <-done:
		// Good, Start completed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Start did not complete after advancing clock")
	}

	require.Error(t, startErr)
	assert.Contains(t, startErr.Error(), "handshake timeout")
	assert.Equal(t, CapsuleStateCrashed, cm.GetState())
}

func TestCapsuleManager_Start_InvalidPath(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/nonexistent/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	err := cm.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start capsule process")
	assert.Equal(t, CapsuleStateCrashed, cm.GetState())
}

func TestCapsuleManager_Stop_NotStarted(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	// Stop without starting should work (idempotent)
	err := cm.Stop()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateStopped, cm.GetState())
}

func TestCapsuleManager_Stop_AlreadyStopped(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	// Stop once
	err := cm.Stop()
	require.NoError(t, err)

	// Stop again (should be idempotent)
	err = cm.Stop()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateStopped, cm.GetState())
}

func TestCapsuleManager_CallTool_NotReady(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	ctx := context.Background()
	input := map[string]any{"arg1": "value1"}

	_, err := cm.CallTool(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capsule is not ready")
}

func TestCapsuleManager_CallTool_StdinUnavailable(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	// Manually set state to READY but leave stdin as nil
	cm.stateMu.Lock()
	cm.state = CapsuleStateReady
	cm.stateMu.Unlock()

	ctx := context.Background()
	input := map[string]any{"arg1": "value1"}

	_, err := cm.CallTool(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin pipe is not available")
}

func TestCapsuleManager_CallTool_ContextTimeout(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManagerWithClock(tool, fakeClock)

	// Manually set up a ready state with mock stdin
	cm.stateMu.Lock()
	cm.state = CapsuleStateReady
	cm.stateMu.Unlock()

	// Create a mock stdin that will block
	cmd := exec.Command("cat")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	cm.processMu.Lock()
	cm.stdin = stdin
	cm.processMu.Unlock()

	// Create a context with timeout using clockwork
	timeout := 10 * time.Millisecond
	ctx, cancel := clockwork.WithTimeout(context.Background(), fakeClock, timeout)
	defer cancel()

	input := map[string]any{"arg1": "value1"}

	// Call CallTool in a goroutine
	done := make(chan bool)
	var callErr error
	go func() {
		_, callErr = cm.CallTool(ctx, input)
		done <- true
	}()

	// Wait for the context to have waiters
	blockCtx, blockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer blockCancel()
	err = fakeClock.BlockUntilContext(blockCtx, 1)
	require.NoError(t, err, "Failed to block until context has waiters")

	// Advance the fake clock past the timeout
	fakeClock.Advance(20 * time.Millisecond)

	// Wait for CallTool to complete
	select {
	case <-done:
		// Good, CallTool completed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("CallTool did not complete after advancing clock")
	}

	require.Error(t, callErr)
	assert.Contains(t, callErr.Error(), "request timeout")

	// Cleanup
	_ = stdin.Close() //nolint:errcheck // cleanup in test
}

func TestCapsuleManager_CallTool_CapsuleContextCancelled(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	// Manually set up a ready state with mock stdin
	cm.stateMu.Lock()
	cm.state = CapsuleStateReady
	cm.stateMu.Unlock()

	// Create a mock stdin
	cmd := exec.Command("cat")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	cm.processMu.Lock()
	cm.stdin = stdin
	cm.processMu.Unlock()

	// Cancel the capsule context
	cm.cancel()

	ctx := context.Background()
	input := map[string]any{"arg1": "value1"}

	_, err = cm.CallTool(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capsule context cancelled")

	// Cleanup
	_ = stdin.Close() //nolint:errcheck // cleanup in test
}

// Test helper: create a simple echo-based capsule that sends handshake
func createEchoCapsuleScript(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
		return ""
	}

	scriptContent := `#!/bin/sh
# Send handshake immediately
echo '{"jsonrpc":"2.0","method":"orla.hello","params":{"name":"test-tool","version":"1.0.0","capabilities":["tools"]}}'
# Then echo back any input (for testing)
cat
`

	scriptFile := filepath.Join(t.TempDir(), "echo-capsule.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptFile, []byte(scriptContent), 0755)
	require.NoError(t, err)

	return scriptFile
}

func TestCapsuleManager_Start_Success(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	scriptPath := createEchoCapsuleScript(t)
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptPath,
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
		},
	}

	cm := NewCapsuleManager(tool)

	err := cm.Start()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateReady, cm.GetState())
	assert.True(t, cm.IsReady())

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

func TestCapsuleManager_Start_WithInterpreter(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	scriptPath := createEchoCapsuleScript(t)
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
		},
	}

	cm := NewCapsuleManager(tool)

	err := cm.Start()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateReady, cm.GetState())

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

func TestCapsuleManager_Start_WithRuntimeArgs(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	scriptPath := createEchoCapsuleScript(t)
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptPath,
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
			Args:             []string{"--test-arg", "value"},
		},
	}

	cm := NewCapsuleManager(tool)

	err := cm.Start()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateReady, cm.GetState())

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

func TestCapsuleManager_Start_WithRuntimeEnv(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	scriptPath := createEchoCapsuleScript(t)
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptPath,
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
			Env: map[string]string{
				"TEST_VAR": "test_value",
			},
		},
	}

	cm := NewCapsuleManager(tool)

	err := cm.Start()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateReady, cm.GetState())

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

// Test helper: create a capsule script that responds to JSON-RPC calls
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

func TestCapsuleManager_CallTool_Success(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	scriptPath := createRespondingCapsuleScript(t)
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptPath,
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
		},
	}

	cm := NewCapsuleManager(tool)

	// Start the capsule
	err := cm.Start()
	require.NoError(t, err)
	assert.Equal(t, CapsuleStateReady, cm.GetState())

	// Call the tool
	ctx := context.Background()
	input := map[string]any{"arg1": "value1", "arg2": "value2"}

	response, err := cm.CallTool(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, "2.0", response.JSONRPC)
	assert.NotZero(t, response.ID)
	assert.NotNil(t, response.Result)

	// Verify result structure
	resultMap, ok := response.Result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test result", resultMap["output"])

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

func TestCapsuleManager_CallTool_JSONRPCError(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	// Create a script that sends an error response
	scriptContent := `#!/bin/sh
# Send handshake
echo '{"jsonrpc":"2.0","method":"orla.hello","params":{"name":"test-tool","version":"1.0.0","capabilities":["tools"]}}'

# Read and respond with error
while IFS= read -r line; do
  REQ_ID=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  if [ -n "$REQ_ID" ]; then
    echo "{\"jsonrpc\":\"2.0\",\"id\":$REQ_ID,\"error\":{\"code\":-32000,\"message\":\"Test error\"}}"
  fi
done
`

	scriptFile := filepath.Join(t.TempDir(), "error-capsule.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptFile, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptFile,
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
		},
	}

	cm := NewCapsuleManager(tool)

	// Start the capsule
	err = cm.Start()
	require.NoError(t, err)

	// Call the tool
	ctx := context.Background()
	input := map[string]any{"arg1": "value1"}

	response, err := cm.CallTool(ctx, input)
	require.NoError(t, err) // CallTool succeeds, but response has error
	require.NotNil(t, response)
	assert.NotNil(t, response.Error)
	assert.Equal(t, -32000, response.Error.Code)
	assert.Equal(t, "Test error", response.Error.Message)

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

func TestCapsuleManager_StateTransitions(t *testing.T) {
	tool := &ToolManifest{
		Name: "test-tool",
		Path: "/path/to/tool",
	}

	cm := NewCapsuleManager(tool)

	// Test state transitions
	assert.Equal(t, CapsuleStateCreated, cm.GetState())

	// Can start from CREATED
	cm.stateMu.Lock()
	cm.state = CapsuleStateStarting
	cm.stateMu.Unlock()
	assert.Equal(t, CapsuleStateStarting, cm.GetState())

	// Can transition to READY
	cm.stateMu.Lock()
	cm.state = CapsuleStateReady
	cm.stateMu.Unlock()
	assert.Equal(t, CapsuleStateReady, cm.GetState())
	assert.True(t, cm.IsReady())

	// Can transition to CRASHED
	cm.stateMu.Lock()
	cm.state = CapsuleStateCrashed
	cm.stateMu.Unlock()
	assert.Equal(t, CapsuleStateCrashed, cm.GetState())
	assert.False(t, cm.IsReady())

	// Can transition to STOPPED
	cm.stateMu.Lock()
	cm.state = CapsuleStateStopped
	cm.stateMu.Unlock()
	assert.Equal(t, CapsuleStateStopped, cm.GetState())
	assert.False(t, cm.IsReady())
}

func TestCapsuleManager_RequestIDIncrement(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Windows capsule script tests not implemented")
	}

	scriptPath := createRespondingCapsuleScript(t)
	tool := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Path:        scriptPath,
		Runtime: &RuntimeConfig{
			StartupTimeoutMs: 5000,
		},
	}

	cm := NewCapsuleManager(tool)

	// Start the capsule
	err := cm.Start()
	require.NoError(t, err)

	ctx := context.Background()
	input := map[string]any{"arg": "value"}

	// Make multiple calls and verify request IDs increment
	var lastID int64 = 0
	for range 3 {
		response, err := cm.CallTool(ctx, input)
		require.NoError(t, err)
		require.NotNil(t, response)
		assert.Greater(t, response.ID, lastID)
		lastID = response.ID
	}

	// Cleanup
	_ = cm.Stop() //nolint:errcheck // cleanup in test
}

func TestJSONRPCRequest_Marshal(t *testing.T) {
	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      123,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      "test-tool",
			"arguments": map[string]any{"arg1": "value1"},
		},
	}

	data, err := json.Marshal(request)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"jsonrpc":"2.0"`)
	assert.Contains(t, string(data), `"id":123`)
	assert.Contains(t, string(data), `"method":"tools/call"`)
}

func TestJSONRPCResponse_Unmarshal(t *testing.T) {
	jsonData := `{"jsonrpc":"2.0","id":123,"result":{"output":"test"}}`

	var response JSONRPCResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	require.NoError(t, err)
	assert.Equal(t, "2.0", response.JSONRPC)
	assert.Equal(t, int64(123), response.ID)
	assert.NotNil(t, response.Result)
}

func TestJSONRPCResponse_WithError(t *testing.T) {
	jsonData := `{"jsonrpc":"2.0","id":123,"error":{"code":-32000,"message":"Test error"}}`

	var response JSONRPCResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	require.NoError(t, err)
	assert.Equal(t, "2.0", response.JSONRPC)
	assert.Equal(t, int64(123), response.ID)
	assert.NotNil(t, response.Error)
	assert.Equal(t, -32000, response.Error.Code)
	assert.Equal(t, "Test error", response.Error.Message)
}

func TestOrlaHelloNotification_Unmarshal(t *testing.T) {
	jsonData := `{"jsonrpc":"2.0","method":"orla.hello","params":{"name":"test-tool","version":"1.0.0","capabilities":["tools","resources"]}}`

	var notification OrlaHelloNotification
	err := json.Unmarshal([]byte(jsonData), &notification)
	require.NoError(t, err)
	assert.Equal(t, "2.0", notification.JSONRPC)
	assert.Equal(t, "orla.hello", notification.Method)
	assert.Equal(t, "test-tool", notification.Params.Name)
	assert.Equal(t, "1.0.0", notification.Params.Version)
	assert.Equal(t, []string{"tools", "resources"}, notification.Params.Capabilities)
}
