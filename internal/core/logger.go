package core

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Init initializes zap's global logger
// After calling this, we use zap.L() directly.
// The logger is explicitly configured to write to stderr to avoid interfering
// with tool stdout (especially important in stdio mode where stdout is used for MCP protocol).
func Init(pretty bool) error {
	var config zap.Config

	if pretty {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	// Explicitly set output to stderr to avoid interfering with tool stdout
	// This is critical in stdio mode where stdout is used for MCP protocol
	config.OutputPaths = []string{"stderr"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build()
	if err != nil {
		return fmt.Errorf("failed to build logger: %w", err)
	}

	zap.ReplaceGlobals(logger)
	return nil
}

// LogToolExecution logs a tool execution event using zap's global logger
func LogToolExecution(toolName string, duration float64, err error) {
	fields := []zap.Field{
		zap.String("tool", toolName),
		zap.Float64("duration_seconds", duration),
		zap.Bool("success", err == nil),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		zap.L().Error("Tool execution failed", fields...)
		return
	}

	zap.L().Info("Tool execution completed successfully", fields...)
}

// LogRequest logs an MCP request using zap's global logger
func LogRequest(method string, duration float64, err error) {
	fields := []zap.Field{
		zap.String("method", method),
		zap.Float64("duration_seconds", duration),
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		zap.L().Error("Request failed", fields...)
		return
	}

	zap.L().Info("Request completed successfully", fields...)
}

// LogPanicRecovery logs a recovered panic with stack trace
func LogPanicRecovery(component string, r interface{}) {
	zap.L().Error("Panic recovered",
		zap.String("component", component),
		zap.Any("panic_value", r),
		zap.Stack("stack_trace"),
	)
}

// LogDeferredError takes a function that returns an error, calls it, and logs the error if it is not nil
func LogDeferredError(fn func() error) {
	if err := fn(); err != nil {
		zap.L().Error("Deferred error", zap.Error(err), zap.Stack("stack_trace"))
	}
}

// LogDeferredError1 takes a function that returns an error, calls it with the given argument, and logs the error if it is not nil
func LogDeferredError1[T any](fn func(T) error, arg T) {
	if err := fn(arg); err != nil {
		zap.L().Error("Deferred error", zap.Error(err), zap.Stack("stack_trace"))
	}
}
