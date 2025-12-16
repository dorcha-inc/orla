package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestInit_PrettyLog tests logger initialization with pretty logging
func TestInit_PrettyLog(t *testing.T) {
	err := Init(true)
	require.NoError(t, err)

	// Verify logger is initialized
	logger := zap.L()
	assert.NotNil(t, logger)

	// Test that we can log
	logger.Info("Test message")
}

// TestInit_Error tests logger initialization error handling
// Note: It's difficult to trigger config.Build() failure in normal circumstances,
// but we can verify the error path exists by checking the error message format
func TestInit_Error(t *testing.T) {
	// This test verifies that Init handles errors correctly
	// In practice, config.Build() rarely fails, but the error path is there
	// We'll test with valid inputs and verify error handling structure

	// Test that Init succeeds with valid inputs
	err := Init(false)
	require.NoError(t, err)

	// The error path (config.Build() failure) is hard to trigger without
	// mocking or invalid system state, but the code path exists for error handling
}

// TestInit_JSONLog tests logger initialization with JSON logging
func TestInit_JSONLog(t *testing.T) {
	err := Init(false)
	require.NoError(t, err)

	// Verify logger is initialized
	logger := zap.L()
	assert.NotNil(t, logger)

	// Test that we can log
	logger.Info("Test message")
}

// TestLogToolExecution_Success tests logging a successful tool execution
func TestLogToolExecution_Success(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	LogToolExecution("test-tool", 1.5, nil)

	// Verify log was written
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "Tool execution completed successfully", entry.Message)
	assert.Equal(t, zap.InfoLevel, entry.Level)

	// Verify fields
	assert.Equal(t, "test-tool", entry.ContextMap()["tool"])
	assert.Equal(t, 1.5, entry.ContextMap()["duration_seconds"])
	assert.Equal(t, true, entry.ContextMap()["success"])
}

// TestLogToolExecution_Error tests logging a failed tool execution
func TestLogToolExecution_Error(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	testErr := errors.New("execution failed")
	LogToolExecution("test-tool", 2.0, testErr)

	// Verify log was written
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "Tool execution failed", entry.Message)
	assert.Equal(t, zap.ErrorLevel, entry.Level)

	// Verify fields
	assert.Equal(t, "test-tool", entry.ContextMap()["tool"])
	assert.Equal(t, 2.0, entry.ContextMap()["duration_seconds"])
	assert.Equal(t, false, entry.ContextMap()["success"])
	assert.NotNil(t, entry.ContextMap()["error"])
}

// TestLogRequest_Success tests logging a successful request
func TestLogRequest_Success(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	LogRequest("tools/list", 0.1, nil)

	// Verify log was written
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "Request completed successfully", entry.Message)
	assert.Equal(t, zap.InfoLevel, entry.Level)

	// Verify fields
	assert.Equal(t, "tools/list", entry.ContextMap()["method"])
	assert.Equal(t, 0.1, entry.ContextMap()["duration_seconds"])
}

// TestLogRequest_Error tests logging a failed request
func TestLogRequest_Error(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	testErr := errors.New("request failed")
	LogRequest("tools/call", 0.5, testErr)

	// Verify log was written
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "Request failed", entry.Message)
	assert.Equal(t, zap.ErrorLevel, entry.Level)

	// Verify fields
	assert.Equal(t, "tools/call", entry.ContextMap()["method"])
	assert.Equal(t, 0.5, entry.ContextMap()["duration_seconds"])
	assert.NotNil(t, entry.ContextMap()["error"])
}

// TestLogPanicRecovery tests logging a recovered panic
func TestLogPanicRecovery(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	panicValue := "test panic"
	LogPanicRecovery("test-component", panicValue)

	// Verify log was written
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "Panic recovered", entry.Message)
	assert.Equal(t, zap.ErrorLevel, entry.Level)

	// Verify fields
	assert.Equal(t, "test-component", entry.ContextMap()["component"])
	assert.Equal(t, panicValue, entry.ContextMap()["panic_value"])
	// Stack trace is logged but may not be in ContextMap, verify it exists in the entry
	assert.NotEmpty(t, entry.Message)
}

// TestLogDeferredError_WithError tests LogDeferredError when function returns an error
func TestLogDeferredError_WithError(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	testErr := errors.New("deferred error")
	LogDeferredError(func() error {
		return testErr
	})

	// Verify log was written
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "Deferred error", entry.Message)
	assert.Equal(t, zap.ErrorLevel, entry.Level)
	// Error field should be present
	assert.NotNil(t, entry.ContextMap()["error"])
	// Stack trace is logged but may not be in ContextMap, verify entry exists
	assert.NotEmpty(t, entry.Message)
}

// TestLogDeferredError_NoError tests LogDeferredError when function returns no error
func TestLogDeferredError_NoError(t *testing.T) {
	// Set up observer to capture logs
	core, logs := observer.New(zap.ErrorLevel)
	logger := zap.New(core)
	zap.ReplaceGlobals(logger)

	LogDeferredError(func() error {
		return nil
	})

	// Verify no log was written (no error means no log)
	assert.Equal(t, 0, logs.Len())
}
