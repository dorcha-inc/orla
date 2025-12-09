package core

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/dorcha-inc/orla/internal/state"
	"github.com/jonboulle/clockwork"
)

// CommandRunner is an interface for running commands, allowing for testing with mocks
type CommandRunner interface {
	CommandContext(ctx context.Context, name string, arg ...string) Command
}

// Command is an interface for exec.Cmd, allowing for testing with mocks
type Command interface {
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	SetStdin(io.Reader)
	Start() error
	Wait() error
}

// execCommand wraps exec.Cmd to implement Command interface
type execCommand struct {
	*exec.Cmd
}

func (e *execCommand) SetStdin(r io.Reader) {
	e.Cmd.Stdin = r
}

// Explicitly forward methods from *exec.Cmd to satisfy the Command interface
// (even though they're already available through embedding, this makes it explicit for the linter)
func (e *execCommand) Start() error {
	return e.Cmd.Start()
}

func (e *execCommand) Wait() error {
	return e.Cmd.Wait()
}

func (e *execCommand) StdinPipe() (io.WriteCloser, error) {
	return e.Cmd.StdinPipe()
}

func (e *execCommand) StdoutPipe() (io.ReadCloser, error) {
	return e.Cmd.StdoutPipe()
}

func (e *execCommand) StderrPipe() (io.ReadCloser, error) {
	return e.Cmd.StderrPipe()
}

// Interface guard for execCommand
var _ Command = &execCommand{}

// execCommandRunner wraps exec.CommandContext to implement CommandRunner
type execCommandRunner struct{}

func (e *execCommandRunner) CommandContext(ctx context.Context, name string, arg ...string) Command {
	return &execCommand{Cmd: exec.CommandContext(ctx, name, arg...)}
}

// Interface guard for execCommandRunner
var _ CommandRunner = &execCommandRunner{}

// OrlaToolExecutor handles tool execution
type OrlaToolExecutor struct {
	timeout       time.Duration
	clock         clockwork.Clock
	commandRunner CommandRunner
}

// NewOrlaToolExecutor creates a new tool executor with a real clock
func NewOrlaToolExecutor(timeoutSeconds int) *OrlaToolExecutor {
	return NewOrlaToolExecutorWithClock(timeoutSeconds, clockwork.NewRealClock())
}

// NewOrlaToolExecutorWithClock creates a new tool executor with a custom clock
// This is useful for testing with a fake clock
func NewOrlaToolExecutorWithClock(timeoutSeconds int, clock clockwork.Clock) *OrlaToolExecutor {
	return &OrlaToolExecutor{
		timeout:       time.Duration(timeoutSeconds) * time.Second,
		clock:         clock,
		commandRunner: &execCommandRunner{},
	}
}

// NewOrlaToolExecutorWithClockAndRunner creates a new tool executor with a custom clock and command runner
// This is useful for testing with a fake clock and mocked command execution
func NewOrlaToolExecutorWithClockAndRunner(timeoutSeconds int, clock clockwork.Clock, runner CommandRunner) *OrlaToolExecutor {
	return &OrlaToolExecutor{
		timeout:       time.Duration(timeoutSeconds) * time.Second,
		clock:         clock,
		commandRunner: runner,
	}
}

// OrlaToolExecutionResult represents the result of a tool execution
type OrlaToolExecutionResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    error  `json:"-"`
}

// Execute executes a tool with the given arguments and input
func (e *OrlaToolExecutor) Execute(ctx context.Context, tool *state.ToolEntry, args []string, stdin string) (*OrlaToolExecutionResult, error) {
	// Create context with timeout using the clock
	execCtx, cancel := clockwork.WithTimeout(ctx, e.clock, e.timeout)
	defer cancel()

	// Build command
	var cmd Command
	if tool.Interpreter != "" {
		// Script with interpreter
		cmdArgs := []string{tool.Path}
		cmdArgs = append(cmdArgs, args...)
		cmd = e.commandRunner.CommandContext(execCtx, tool.Interpreter, cmdArgs...)
	} else {
		// Binary executable
		cmd = e.commandRunner.CommandContext(execCtx, tool.Path, args...)
	}

	// Set up stdin
	if stdin != "" {
		cmd.SetStdin(strings.NewReader(stdin))
	}

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Read output
	var stdoutBuf, stderrBuf strings.Builder
	done := make(chan error, 2)

	go func() {
		_, err := io.Copy(&stdoutBuf, stdout)
		done <- err
	}()

	go func() {
		_, err := io.Copy(&stderrBuf, stderr)
		done <- err
	}()

	// Wait for output reading to complete
	<-done
	<-done

	// Wait for command to finish
	err = cmd.Wait()

	result := &OrlaToolExecutionResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.Error = err
			return result, err
		}
	}

	// Check for context timeout
	if execCtx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Errorf("tool execution timed out after %v", e.timeout)
		return result, result.Error
	}

	return result, nil
}
