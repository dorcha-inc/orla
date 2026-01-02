package testing

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dorcha-inc/orla/internal/core"
)

type CapturedOutput struct {
	OriginalStdout *os.File
	OriginalStderr *os.File
	CapturedStdout *os.File // Read end
	CapturedStderr *os.File // Read end
	stdoutW        *os.File // Write end (needed to close for ReadAll to complete)
	stderrW        *os.File // Write end (needed to close for ReadAll to complete)
}

// NewCapturedOutput captures both stdout and stderr output and returns them separately
// Returns *CapturedOutput and error
func NewCapturedOutput() (*CapturedOutput, error) {
	originalStdout := os.Stdout
	originalStderr := os.Stderr

	// Create separate pipes for stdout and stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		core.LogDeferredError(stdoutR.Close)
		core.LogDeferredError(stdoutW.Close)
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	return &CapturedOutput{
		OriginalStdout: originalStdout,
		OriginalStderr: originalStderr,
		CapturedStdout: stdoutR, // Read end for stdout
		CapturedStderr: stderrR, // Read end for stderr
		stdoutW:        stdoutW, // Write end (stored so we can close it)
		stderrW:        stderrW, // Write end (stored so we can close it)
	}, nil
}

// Stop stops capturing output and returns the captured output
func (capturedOutput *CapturedOutput) Stop() (string, string, error) {
	// Restore original streams first so any goroutines writing will write to original streams
	os.Stdout = capturedOutput.OriginalStdout
	os.Stderr = capturedOutput.OriginalStderr

	// Close write ends to signal EOF to ReadAll
	// This must happen after restoring streams so goroutines don't try to write to closed pipes
	core.LogDeferredError(capturedOutput.stdoutW.Close)
	core.LogDeferredError(capturedOutput.stderrW.Close)

	// Small delay to ensure any pending writes complete
	// This is especially important for goroutines that might be writing
	time.Sleep(10 * time.Millisecond)

	// Now read from the read ends
	capturedStdout, err := io.ReadAll(capturedOutput.CapturedStdout)
	if err != nil {
		core.LogDeferredError(capturedOutput.CapturedStdout.Close)
		core.LogDeferredError(capturedOutput.CapturedStderr.Close)
		return "", "", fmt.Errorf("failed to read captured stdout: %w", err)
	}

	capturedStderr, err := io.ReadAll(capturedOutput.CapturedStderr)
	if err != nil {
		core.LogDeferredError(capturedOutput.CapturedStdout.Close)
		core.LogDeferredError(capturedOutput.CapturedStderr.Close)
		return "", "", fmt.Errorf("failed to read captured stderr: %w", err)
	}

	// Close read ends
	core.LogDeferredError(capturedOutput.CapturedStdout.Close)
	core.LogDeferredError(capturedOutput.CapturedStderr.Close)

	return string(capturedStdout), string(capturedStderr), nil
}
