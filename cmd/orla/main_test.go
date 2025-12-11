package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/dorcha-inc/orla/internal/server"
	"github.com/dorcha-inc/orla/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseFlags tests flag parsing
func TestParseFlags(t *testing.T) {
	// Save original args and restore after test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test config flag
	os.Args = []string{"orla", "-config", "/path/to/config.json"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags := parseFlags()
	assert.Equal(t, "/path/to/config.json", flags.configPath)
	assert.False(t, flags.useStdio)
	assert.False(t, flags.prettyLog)
	assert.Equal(t, 0, flags.portFlag)

	// Test stdio flag
	os.Args = []string{"orla", "-stdio"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags = parseFlags()
	assert.True(t, flags.useStdio)

	// Test port flag
	os.Args = []string{"orla", "-port", "9090"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags = parseFlags()
	assert.Equal(t, 9090, flags.portFlag)

	// Test pretty flag
	os.Args = []string{"orla", "-pretty"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags = parseFlags()
	assert.True(t, flags.prettyLog)
}

// TestLoadConfig tests configuration loading
func TestLoadConfig(t *testing.T) {
	// Test with empty path (should use default)
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	cfg, err := loadConfig("")
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, 30, cfg.Timeout)

	// Test with valid config file
	configPath := filepath.Join(tmpDir, "orla.json")
	configContent := `{
		"tools_dir": "tools",
		"port": 9090,
		"timeout": 60,
		"log_format": "json",
		"log_level": "info"
	}`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg2, err := loadConfig(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg2)
	assert.Equal(t, 9090, cfg2.Port)
	assert.Equal(t, 60, cfg2.Timeout)

	// Test with non-existent config file
	_, err = loadConfig("/nonexistent/config.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestResolveLogFormat tests log format resolution logic
func TestResolveLogFormat(t *testing.T) {
	// Test: prettyLog flag wins
	cfg := &state.OrlaConfig{LogFormat: "json"}
	prettyLog := true
	resolved := resolveLogFormat(cfg, prettyLog)
	assert.True(t, resolved)

	// Test: config.LogFormat = "pretty" when flag is false
	cfg = &state.OrlaConfig{LogFormat: "pretty"}
	prettyLog = false
	resolved = resolveLogFormat(cfg, prettyLog)
	assert.True(t, resolved)

	// Test: config.LogFormat != "pretty" and flag is false
	cfg = &state.OrlaConfig{LogFormat: "json"}
	prettyLog = false
	resolved = resolveLogFormat(cfg, prettyLog)
	assert.False(t, resolved)
}

// TestValidateAndApplyPort tests port validation and application logic
func TestValidateAndApplyPort(t *testing.T) {
	// Test valid ports
	cfg := &state.OrlaConfig{Port: 8080}
	err := validateAndApplyPort(cfg, 0, false)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)

	// Test port override
	cfg = &state.OrlaConfig{Port: 8080}
	err = validateAndApplyPort(cfg, 9090, false)
	assert.NoError(t, err)
	assert.Equal(t, 9090, cfg.Port)

	// Test default port when unset
	cfg = &state.OrlaConfig{Port: 0}
	err = validateAndApplyPort(cfg, 0, false)
	assert.NoError(t, err)
	assert.Equal(t, 8080, cfg.Port)

	// Test invalid port (negative)
	cfg = &state.OrlaConfig{Port: 8080}
	err = validateAndApplyPort(cfg, -1, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port must be a positive integer")

	// Test stdio mode (port doesn't matter)
	cfg = &state.OrlaConfig{Port: 8080}
	err = validateAndApplyPort(cfg, 0, true)
	assert.NoError(t, err)
	// Port should remain unchanged when stdio is used
	assert.Equal(t, 8080, cfg.Port)
}

// TestSetupSignalHandling tests signal handling setup
func TestSetupSignalHandling(t *testing.T) {
	cfg := &state.OrlaConfig{
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
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	cfg, err := state.NewDefaultOrlaConfig()
	require.NoError(t, err)

	srv := server.NewOrlaServer(cfg, "")

	// Test that runServer function exists and accepts parameters correctly
	// We use a cancelled context to ensure it returns quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Test stdio mode - function should handle cancelled context
	// Note: This may not error immediately, but tests the code path exists
	err = runServer(ctx, srv, true, cfg)
	assert.NoError(t, err)

	// Test HTTP mode - function should handle cancelled context
	// Note: This may not error immediately, but tests the code path exists
	err = runServer(ctx, srv, false, cfg)
	assert.NoError(t, err)
}

// TestTestableMain tests the main application logic
func TestTestableMain(t *testing.T) {
	// Save original args and restore after test
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test with valid default configuration
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Set up minimal flags (no flags = defaults)
	os.Args = []string{"orla"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Use a cancelled context so the server doesn't actually start
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// testableMain will try to start a server, but with a cancelled context it should
	// return gracefully.
	err = testableMain(ctx)
	assert.NoError(t, err)
}

// TestTestableMain_ConfigError tests error handling when config loading fails
func TestTestableMain_ConfigError(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set up flags with non-existent config file
	os.Args = []string{"orla", "-config", "/nonexistent/config.json"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	ctx := context.Background()
	err := testableMain(ctx)
	assert.Error(t, err)
	// The error message comes from loadConfig, which wraps the error
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestTestableMain_PortValidationError tests error handling when port validation fails
func TestTestableMain_PortValidationError(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Set up flags with invalid port
	os.Args = []string{"orla", "-port", "-1"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	ctx := context.Background()
	err = testableMain(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port must be a positive integer")
}

// TestTestableMain_WithConfigFile tests with a valid config file
// Note: This test verifies that testableMain can load a config file and initialize correctly.
// We test the initialization path but skip full server startup (which would call zap.L().Fatal()
// and terminate the process). Full server startup testing should be done via integration tests.
func TestTestableMain_WithConfigFile(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create a valid config file
	configContent := `{
		"tools_dir": "tools",
		"port": 0,
		"timeout": 60,
		"log_format": "json",
		"log_level": "info"
	}`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Set up flags with config file
	os.Args = []string{"orla", "-config", configPath, "-stdio"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Verify that loadConfig works with this file (testing the initialization path)
	cfg, err := loadConfig(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 60, cfg.Timeout)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = testableMain(ctx)
	assert.NoError(t, err)
}
