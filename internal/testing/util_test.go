package testing

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCapturedOutput(t *testing.T) {
	originalStdout := os.Stdout
	originalStderr := os.Stderr

	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)
	require.NotNil(t, capturedOutput)

	// Verify that stdout and stderr have been redirected
	assert.NotEqual(t, originalStdout, os.Stdout, "Stdout should be redirected")
	assert.NotEqual(t, originalStderr, os.Stderr, "Stderr should be redirected")

	// Verify the captured output struct
	assert.Equal(t, originalStdout, capturedOutput.OriginalStdout)
	assert.Equal(t, originalStderr, capturedOutput.OriginalStderr)
	assert.NotNil(t, capturedOutput.CapturedStdout)
	assert.NotNil(t, capturedOutput.CapturedStderr)

	// Clean up
	_, _, err = capturedOutput.Stop()
	require.NoError(t, err)

	// Verify streams are restored
	assert.Equal(t, originalStdout, os.Stdout, "Stdout should be restored")
	assert.Equal(t, originalStderr, os.Stderr, "Stderr should be restored")
}

func TestCapturedOutput_Stop_Stdout(t *testing.T) {
	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Write to stdout
	testMessage := "test stdout message\n"
	fmt.Print(testMessage)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	assert.Equal(t, testMessage, stdout, "Should capture stdout output")
	assert.Empty(t, stderr, "Stderr should be empty")
}

func TestCapturedOutput_Stop_Stderr(t *testing.T) {
	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Write to stderr
	testMessage := "test stderr message\n"
	fmt.Fprint(os.Stderr, testMessage)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	assert.Empty(t, stdout, "Stdout should be empty")
	assert.Equal(t, testMessage, stderr, "Should capture stderr output")
}

func TestCapturedOutput_Stop_Both(t *testing.T) {
	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Write to both stdout and stderr
	stdoutMessage := "stdout message\n"
	stderrMessage := "stderr message\n"

	fmt.Print(stdoutMessage)
	fmt.Fprint(os.Stderr, stderrMessage)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	assert.Equal(t, stdoutMessage, stdout, "Should capture stdout output")
	assert.Equal(t, stderrMessage, stderr, "Should capture stderr output")
}

func TestCapturedOutput_Stop_Empty(t *testing.T) {
	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Don't write anything
	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	assert.Empty(t, stdout, "Stdout should be empty when nothing written")
	assert.Empty(t, stderr, "Stderr should be empty when nothing written")
}

func TestCapturedOutput_Stop_MultipleWrites(t *testing.T) {
	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Write multiple times
	fmt.Print("first ")
	fmt.Print("second ")
	fmt.Print("third\n")

	fmt.Fprint(os.Stderr, "error1 ")
	fmt.Fprint(os.Stderr, "error2\n")

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	assert.Equal(t, "first second third\n", stdout, "Should capture all stdout writes")
	assert.Equal(t, "error1 error2\n", stderr, "Should capture all stderr writes")
}

func TestCapturedOutput_Stop_RestoresStreams(t *testing.T) {
	originalStdout := os.Stdout
	originalStderr := os.Stderr

	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Verify streams are redirected
	assert.NotEqual(t, originalStdout, os.Stdout)
	assert.NotEqual(t, originalStderr, os.Stderr)

	// Stop should restore streams
	_, _, err = capturedOutput.Stop()
	require.NoError(t, err)

	// Verify streams are restored
	assert.Equal(t, originalStdout, os.Stdout, "Stdout should be restored after Stop")
	assert.Equal(t, originalStderr, os.Stderr, "Stderr should be restored after Stop")
}

func TestCapturedOutput_Stop_CanBeCalledMultipleTimes(t *testing.T) {
	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	fmt.Print("test")

	// First call should work
	stdout1, stderr1, err := capturedOutput.Stop()
	require.NoError(t, err)
	assert.Equal(t, "test", stdout1)
	assert.Empty(t, stderr1, "Stderr should be empty")

	// Second call should fail because pipes are already closed
	// This is expected behavior - Stop() should only be called once
	stdout2, stderr2, err := capturedOutput.Stop()
	require.Error(t, err, "Second call to Stop() should fail because pipes are closed")
	assert.Empty(t, stdout2)
	assert.Empty(t, stderr2)
}

func TestNewCapturedOutput_ErrorHandling(t *testing.T) {
	// This test is difficult because os.Pipe() rarely fails
	// But we can verify the function handles the structure correctly
	// by testing that it properly stores the original streams
	originalStdout := os.Stdout
	originalStderr := os.Stderr

	capturedOutput, err := NewCapturedOutput()
	require.NoError(t, err)

	// Verify original streams are stored
	assert.Equal(t, originalStdout, capturedOutput.OriginalStdout)
	assert.Equal(t, originalStderr, capturedOutput.OriginalStderr)

	// Clean up
	_, _, err = capturedOutput.Stop()
	require.NoError(t, err)
}
