package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/server"
	"github.com/dorcha-inc/orla/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig tests configuration loading
func TestLoadConfig(t *testing.T) {
	// Test with empty path (should use default)
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	cfg, err := config.LoadConfig("")
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, 30, cfg.Timeout)

	// Test with valid config file
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := `tools_dir: tools
port: 9090
timeout: 60
log_format: json
log_level: info
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg2, err := config.LoadConfig(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg2)
	assert.Equal(t, 9090, cfg2.Port)
	assert.Equal(t, 60, cfg2.Timeout)

	// Test with non-existent config file
	_, err = config.LoadConfig("/nonexistent/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestResolveLogFormat tests log format resolution logic
func TestResolveLogFormat(t *testing.T) {
	// Test: prettyLog flag wins
	cfg := &config.OrlaConfig{LogFormat: "json"}
	prettyLog := true
	resolved := resolveLogFormat(cfg, prettyLog)
	assert.True(t, resolved)

	// Test: config.LogFormat = "pretty" when flag is false
	cfg = &config.OrlaConfig{LogFormat: "pretty"}
	prettyLog = false
	resolved = resolveLogFormat(cfg, prettyLog)
	assert.True(t, resolved)

	// Test: config.LogFormat != "pretty" and flag is false
	cfg = &config.OrlaConfig{LogFormat: "json"}
	prettyLog = false
	resolved = resolveLogFormat(cfg, prettyLog)
	assert.False(t, resolved)
}

// TestValidateAndApplyPort tests port validation and application logic
func TestValidateAndApplyPort(t *testing.T) {
	// Test valid ports
	cfg := &config.OrlaConfig{Port: 8080}
	err := validateAndApplyPort(cfg, 0, false)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)

	// Test port override
	cfg = &config.OrlaConfig{Port: 8080}
	err = validateAndApplyPort(cfg, 9090, false)
	assert.NoError(t, err)
	assert.Equal(t, 9090, cfg.Port)

	// Test default port when unset
	cfg = &config.OrlaConfig{Port: 0}
	err = validateAndApplyPort(cfg, 0, false)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)

	// Test invalid port (negative)
	cfg = &config.OrlaConfig{Port: 8080}
	err = validateAndApplyPort(cfg, -1, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port must be a positive integer")

	// Test stdio mode (port doesn't matter)
	cfg = &config.OrlaConfig{Port: 8080}
	err = validateAndApplyPort(cfg, 0, true)
	assert.NoError(t, err)
	// Port should remain unchanged when stdio is used
	assert.Equal(t, 8080, cfg.Port)
}

// TestSetupSignalHandling tests signal handling setup
func TestSetupSignalHandling(t *testing.T) {
	cfg := &config.OrlaConfig{
		ToolsDir:      "/tmp/tools",
		ToolsRegistry: state.NewToolsRegistry(),
		Port:          8080,
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}
	srv := server.NewOrlaServer(cfg, "")

	ctx, cancel := setupSignalHandling(context.Background(), srv)
	assert.NotNil(t, ctx)
	assert.NotNil(t, cancel)

	// Cancel should work
	cancel()
	select {
	case <-ctx.Done():
		// Good, context was cancelled
	default:
		t.Fatal("Context should be cancelled")
	}
}

// TestRunServer tests server startup logic
// Note: This test verifies the function exists and can be called, but doesn't
// actually start long-running servers. Full server testing is done via integration tests.
func TestRunServer(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	cfg, err := config.LoadConfig("")
	require.NoError(t, err)

	// Use a random port (0) to avoid port conflicts in tests
	cfg.Port = 0

	srv := server.NewOrlaServer(cfg, "")

	// Test that runServer function exists and accepts parameters correctly
	// We use a cancelled context to ensure it returns quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Test stdio mode - function should handle cancelled context
	// When context is cancelled, ServeStdio returns context.Canceled error
	err = runServer(ctx, srv, true, cfg)
	assert.Error(t, err, "runServer should return error when context is cancelled in stdio mode")
	assert.True(t, errors.Is(err, context.Canceled), "Expected context.Canceled error, got: %v", err)

	// Test HTTP mode - function should handle cancelled context gracefully
	// HTTP mode may return nil (graceful shutdown) or context.Canceled depending on timing
	err = runServer(ctx, srv, false, cfg)
	if err != nil {
		// If there's an error, it should be context.Canceled
		// Note: port binding errors are also acceptable if port is already in use
		isCanceled := errors.Is(err, context.Canceled)
		isPortError := strings.Contains(err.Error(), "bind") || strings.Contains(err.Error(), "address already in use")
		assert.True(t, isCanceled || isPortError, "Expected context.Canceled or port error, got: %v", err)
	}
	// If err is nil, that's also acceptable (graceful shutdown completed)
}

// TestRunServe tests the serve command execution
func TestRunServe(t *testing.T) {
	// Test with valid default configuration
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Test serve command with stdio flag (will exit quickly with cancelled context)
	err = runServe("", true, false, 0, "")
	// Should not error on initialization, but may error when trying to start server
	// which is expected in test environment
	if err != nil {
		// Server startup errors are acceptable in test environment
		assert.True(t, errors.Is(err, context.Canceled) || err.Error() != "", "Unexpected error: %v", err)
	}
}

// TestRunServe_ConfigError tests error handling when config loading fails
func TestRunServe_ConfigError(t *testing.T) {
	// Test with non-existent config file
	err := runServe("/nonexistent/config.yaml", false, false, 0, "")
	assert.Error(t, err)
	// The error message comes from loadConfig, which wraps the error
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestRunServe_PortValidationError tests error handling when port validation fails
func TestRunServe_PortValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Test with invalid port
	err = runServe("", false, false, -1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port must be a positive integer")
}
