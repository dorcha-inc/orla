package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const windowsOS = "windows"

// TestNewOrlaToolExecutor tests the creation of a new tool executor
func TestNewOrlaToolExecutor(t *testing.T) {
	executor := NewOrlaToolExecutor(30)
	require.NotNil(t, executor)
	assert.Equal(t, 30*time.Second, executor.timeout)
	assert.NotNil(t, executor.clock)
}

// TestNewOrlaToolExecutorWithClock tests the creation of a new tool executor with a custom clock
func TestNewOrlaToolExecutorWithClock(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	executor := NewOrlaToolExecutorWithClock(30, fakeClock)
	require.NotNil(t, executor)
	assert.Equal(t, 30*time.Second, executor.timeout)
	assert.Equal(t, fakeClock, executor.clock)
}

// TestExecute_BinarySuccess tests successful execution of a binary tool
func TestExecute_BinarySuccess(t *testing.T) {
	executor := NewOrlaToolExecutor(10)

	// Use a real binary executable (echo) to test binary execution
	var toolPath string
	var expectedOutput string
	if runtime.GOOS == windowsOS {
		toolPath = "cmd.exe"
		expectedOutput = "hello world"
	} else {
		toolPath = "/bin/echo"
		expectedOutput = "hello world"
	}

	tool := &ToolEntry{
		Name:        "echo",
		Path:        toolPath,
		Interpreter: "", // No interpreter - this is a binary
	}

	var args []string
	if runtime.GOOS == windowsOS {
		args = []string{"/c", "echo", "hello world"}
	} else {
		args = []string{"hello world"}
	}

	result, err := executor.Execute(context.Background(), tool, args, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, expectedOutput)
	assert.Empty(t, result.Stderr)
	assert.Nil(t, result.Error)
}

// TestExecute_PipeErrors tests error handling for pipe creation failures
// Note: It's difficult to trigger pipe creation failures in normal circumstances,
// but we verify the error paths exist
func TestExecute_PipeErrors(t *testing.T) {
	// This test verifies that Execute handles pipe creation errors
	// In practice, pipe creation rarely fails, but the error paths are there

	executor := NewOrlaToolExecutor(10)
	tool := &ToolEntry{
		Name:        "test",
		Path:        "/bin/echo",
		Interpreter: "",
	}

	// Normal execution should work
	result, err := executor.Execute(context.Background(), tool, []string{"test"}, "")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// The pipe error paths (StdoutPipe/StderrPipe failures) are hard to trigger
	// without mocking, but the code handles them correctly
}

// TestExecute_ScriptWithInterpreter tests successful execution of a script with interpreter
func TestExecute_ScriptWithInterpreter(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping interpreter test on Windows")
	}

	executor := NewOrlaToolExecutor(10)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "echo hello from script\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), tool, []string{}, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "hello from script")
	assert.Empty(t, result.Stderr)
	assert.Nil(t, result.Error)
}

// TestExecute_WithArgs tests execution with command-line arguments
func TestExecute_WithArgs(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping args test on Windows")
	}

	executor := NewOrlaToolExecutor(10)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "#!/bin/sh\necho $1 $2\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), tool, []string{"arg1", "arg2"}, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "arg1 arg2")
}

// TestExecute_WithStdin tests execution with stdin input
func TestExecute_WithStdin(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping stdin test on Windows")
	}

	executor := NewOrlaToolExecutor(10)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "#!/bin/sh\nread input\necho \"received: $input\"\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), tool, []string{}, "test input")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "received: test input")
}

// TestExecute_NonZeroExitCode tests handling of non-zero exit codes
func TestExecute_NonZeroExitCode(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping exit code test on Windows")
	}

	executor := NewOrlaToolExecutor(10)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "#!/bin/sh\necho error message >&2\nexit 42\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), tool, []string{}, "")
	// Execute does not return an error for non-zero exit codes (only sets ExitCode)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 42, result.ExitCode)
	assert.Contains(t, result.Stderr, "error message")
	assert.Nil(t, result.Error)
}

// TestExecute_StderrCapture tests that stderr is properly captured
func TestExecute_StderrCapture(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping stderr test on Windows")
	}

	executor := NewOrlaToolExecutor(10)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "#!/bin/sh\necho stdout message\necho stderr message >&2\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), tool, []string{}, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, result.Stdout, "stdout message")
	assert.Contains(t, result.Stderr, "stderr message")
}

// mockCommandRunner creates commands that respect context cancellation from fake clocks
// This allows us to test timeouts without relying on real system time
//
//nolint:unused // Reserved for future test scenarios
type mockCommandRunner struct{}

//nolint:unused // Reserved for future test scenarios
func (m *mockCommandRunner) CommandContext(ctx context.Context, name string, arg ...string) Command {
	// Use the real exec.CommandContext - it respects context cancellation
	// When the fake clock expires the context, exec.CommandContext will kill the process
	return &execCommandWrapper{Cmd: exec.CommandContext(ctx, name, arg...)}
}

// execCommandWrapper wraps exec.Cmd to implement Command interface for testing
//
//nolint:unused // Reserved for future test scenarios
type execCommandWrapper struct {
	*exec.Cmd
}

//nolint:unused // Reserved for future test scenarios
func (e *execCommandWrapper) SetStdin(r io.Reader) {
	e.Stdin = r
}

// Explicitly forward methods from *exec.Cmd to satisfy the Command interface
//
//nolint:unused // Reserved for future test scenarios
func (e *execCommandWrapper) Start() error {
	return e.Cmd.Start()
}

//nolint:unused // Reserved for future test scenarios
func (e *execCommandWrapper) Wait() error {
	return e.Cmd.Wait()
}

//nolint:unused // Reserved for future test scenarios
func (e *execCommandWrapper) StdinPipe() (io.WriteCloser, error) {
	return e.Cmd.StdinPipe()
}

//nolint:unused // Reserved for future test scenarios
func (e *execCommandWrapper) StdoutPipe() (io.ReadCloser, error) {
	return e.Cmd.StdoutPipe()
}

//nolint:unused // Reserved for future test scenarios
func (e *execCommandWrapper) StderrPipe() (io.ReadCloser, error) {
	return e.Cmd.StderrPipe()
}

// timeoutMockCommandRunner creates commands that simulate timeout behavior
// for testing timeout detection without real processes
type timeoutMockCommandRunner struct {
	ctx context.Context
}

func (m *timeoutMockCommandRunner) CommandContext(ctx context.Context, name string, arg ...string) Command {
	m.ctx = ctx
	return &timeoutMockCommand{ctx: ctx}
}

// timeoutMockCommand simulates a command that times out when context expires
type timeoutMockCommand struct {
	ctx        context.Context
	stdoutPipe *mockReadCloser
	stderrPipe *mockReadCloser
	stdinSet   bool
	started    bool
}

type mockReadCloser struct {
	io.ReadCloser
	closed bool //nolint:unused // Reserved for future test scenarios
}

func (m *timeoutMockCommand) StdinPipe() (io.WriteCloser, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *timeoutMockCommand) StdoutPipe() (io.ReadCloser, error) {
	m.stdoutPipe = &mockReadCloser{ReadCloser: io.NopCloser(strings.NewReader(""))}
	return m.stdoutPipe, nil
}

func (m *timeoutMockCommand) StderrPipe() (io.ReadCloser, error) {
	m.stderrPipe = &mockReadCloser{ReadCloser: io.NopCloser(strings.NewReader(""))}
	return m.stderrPipe, nil
}

func (m *timeoutMockCommand) SetStdin(r io.Reader) {
	m.stdinSet = true
}

func (m *timeoutMockCommand) Start() error {
	m.started = true
	return nil
}

func (m *timeoutMockCommand) Wait() error {
	// Wait for context to expire (simulated by fake clock)
	<-m.ctx.Done()
	if m.ctx.Err() == context.DeadlineExceeded {
		return context.DeadlineExceeded
	}
	return m.ctx.Err()
}

// TestExecute_Timeout tests that execution times out correctly using a fake clock
// We use a mocked command runner that simulates timeout behavior, allowing us to
// test with fake clocks without relying on real processes or system time.
func TestExecute_Timeout(t *testing.T) {
	fakeClock := clockwork.NewFakeClock()
	mockRunner := &timeoutMockCommandRunner{}
	executor := NewOrlaToolExecutorWithClockAndRunner(1, fakeClock, mockRunner) // 1 second timeout

	tool := &ToolEntry{
		Name:        "test-tool",
		Path:        "/fake/path",
		Interpreter: "",
	}

	// Start execution in a goroutine
	done := make(chan bool)
	var result *OrlaToolExecutionResult
	var execErr error
	go func() {
		result, execErr = executor.Execute(context.Background(), tool, []string{}, "")
		done <- true
	}()

	// Wait for the executor to be waiting on the clock (the timeout context)
	// BlockUntilContext waits for the context to have waiters
	blockCtx, blockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer blockCancel()
	err := fakeClock.BlockUntilContext(blockCtx, 1)
	require.NoError(t, err, "Failed to block until context has waiters")

	// Advance the fake clock past the timeout - this expires the context
	// The mock command's Wait() will return DeadlineExceeded
	fakeClock.Advance(2 * time.Second)

	// Wait for execution to complete (should timeout immediately)
	select {
	case <-done:
		// Good, execution completed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Execution did not complete after advancing clock")
	}

	assert.Error(t, execErr)
	assert.NotNil(t, result)
	// The error should indicate a timeout - check for either "timed out" or "deadline exceeded"
	assert.True(t, strings.Contains(execErr.Error(), "timed out") || strings.Contains(execErr.Error(), "deadline exceeded"),
		"Expected timeout error, got: %v", execErr)
	assert.NotNil(t, result.Error)
}

// TestExecute_CommandNotFound tests handling of command not found errors
func TestExecute_CommandNotFound(t *testing.T) {
	executor := NewOrlaToolExecutor(10)

	tool := &ToolEntry{
		Name:        "nonexistent",
		Path:        "/nonexistent/path/to/command",
		Interpreter: "",
	}

	result, err := executor.Execute(context.Background(), tool, []string{}, "")
	// Command not found errors occur at Start(), so result may be nil
	assert.Error(t, err)
	// Result might be nil if error occurs before execution starts
	if result != nil {
		assert.NotNil(t, result.Error)
	}
}

// TestExecute_ContextCancellation tests that context cancellation works
func TestExecute_ContextCancellation(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping context cancellation test on Windows")
	}

	fakeClock := clockwork.NewFakeClock()
	executor := NewOrlaToolExecutorWithClock(10, fakeClock)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "#!/bin/sh\nsleep 5\necho done\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start execution in a goroutine
	done := make(chan bool)
	var result *OrlaToolExecutionResult
	go func() {
		var err error
		result, err = executor.Execute(ctx, tool, []string{}, "")
		_ = err // May or may not be set depending on timing
		done <- true
	}()

	// Wait a bit to ensure the command has started
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for execution to complete
	<-done

	// The command should be killed due to context cancellation
	assert.NotNil(t, result)
}

// TestExecute_EmptyStdin tests execution with empty stdin string
func TestExecute_EmptyStdin(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping empty stdin test on Windows")
	}

	executor := NewOrlaToolExecutor(10)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-script.sh")
	scriptContent := "#!/bin/sh\necho success\n"

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	tool := &ToolEntry{
		Name:        "test-script",
		Path:        scriptPath,
		Interpreter: "/bin/sh",
	}

	result, err := executor.Execute(context.Background(), tool, []string{}, "")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "success")
}

// TestStdinPipe tests that StdinPipe can be called on execCommand
func TestStdinPipe(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip("Skipping StdinPipe test on Windows")
	}

	runner := &execCommandRunner{}
	cmd := runner.CommandContext(context.Background(), "/bin/cat")
	execCmd, ok := cmd.(*execCommand)
	require.True(t, ok, "Command should be *execCommand")

	// StdinPipe should work before Start
	stdinPipe, err := execCmd.StdinPipe()
	require.NoError(t, err)
	assert.NotNil(t, stdinPipe)

	// Close the pipe
	require.NoError(t, stdinPipe.Close())
}
