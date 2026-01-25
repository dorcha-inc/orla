package orla

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// setupObserverLogger creates an observer logger and replaces the global logger
// Returns the observer logs and a cleanup function to restore the original logger
func setupObserverLogger(level zapcore.Level) (*observer.ObservedLogs, func()) {
	core, logs := observer.New(level)
	logger := zap.New(core)
	originalLogger := zap.L()
	zap.ReplaceGlobals(logger)
	return logs, func() { zap.ReplaceGlobals(originalLogger) }
}

func TestLogDeferredError_NoError(t *testing.T) {
	logs, cleanup := setupObserverLogger(zap.InfoLevel)
	defer cleanup()

	// Should not log anything when there's no error
	LogDeferredError(func() error {
		return nil
	})

	assert.Equal(t, 0, logs.Len(), "Should not log anything when there's no error")
}

func TestLogDeferredError_WithError(t *testing.T) {
	logs, cleanup := setupObserverLogger(zap.ErrorLevel)
	defer cleanup()

	// Should log the error
	testErr := errors.New("test error")
	LogDeferredError(func() error {
		return testErr
	})

	// Verify that an error was logged
	assert.Equal(t, 1, logs.Len(), "Should log one error")

	logEntry := logs.All()[0]
	assert.Equal(t, "Deferred error", logEntry.Message)
	assert.Equal(t, zap.ErrorLevel, logEntry.Level)

	// Verify the error field is present
	errField := logEntry.ContextMap()["error"]
	assert.NotNil(t, errField)
	errStr, ok := errField.(string)
	require.True(t, ok, "error field should be a string")
	assert.Contains(t, errStr, "test error")

	// Verify stack trace is present
	stackField := logEntry.ContextMap()["stack_trace"]
	assert.NotNil(t, stackField)
}
