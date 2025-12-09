package core

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Init initializes zap's global logger
// After calling this, we use zap.L() directly.
func Init(pretty bool) error {
	var config zap.Config

	if pretty {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

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
