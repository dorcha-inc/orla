package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDefaultOrlaConfig tests the creation of a default configuration
func TestNewDefaultOrlaConfig(t *testing.T) {
	// Create the tools directory that NewDefaultOrlaConfig expects
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", err)
		}
	}()

	// Change to temp directory and create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err = os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	cfg, err := NewDefaultOrlaConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify default values
	assert.NotEmpty(t, cfg.ToolsDir)
	assert.True(t, filepath.IsAbs(cfg.ToolsDir), "ToolsDir should be absolute")
	assert.NotNil(t, cfg.ToolsRegistry)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, 30, cfg.Timeout)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "info", cfg.LogLevel)
}

// TestNewDefaultOrlaConfig_NonexistentToolsDir tests graceful handling when tools directory doesn't exist
func TestNewDefaultOrlaConfig_NonexistentToolsDir(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", err)
		}
	}()

	// Change to temp directory but don't create tools subdirectory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	// NewDefaultOrlaConfig should succeed even if tools directory doesn't exist (graceful degradation)
	cfg, err := NewDefaultOrlaConfig()
	require.NoError(t, err, "Should succeed even without tools directory")
	assert.NotNil(t, cfg, "Config should be created")
	assert.Empty(t, cfg.ToolsRegistry.ListTools(), "Should have no tools when directory doesn't exist")
}

// TestNewOrlaConfigFromPath_ValidConfig tests loading a valid config file
func TestNewOrlaConfigFromPath_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")

	// Create a tools directory
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create a test tool
	toolPath := filepath.Join(toolsDir, "test-tool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(toolPath, []byte("#!/bin/sh\necho hello\n"), 0755)
	require.NoError(t, err)

	// Create config file
	config := OrlaConfig{
		ToolsDir:  "./tools",
		Port:      9000,
		Timeout:   60,
		LogFormat: "pretty",
		LogLevel:  "debug",
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := NewOrlaConfigFromPath(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify values
	assert.True(t, filepath.IsAbs(cfg.ToolsDir), "ToolsDir should be absolute")
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 60, cfg.Timeout)
	assert.Equal(t, "pretty", cfg.LogFormat)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.NotNil(t, cfg.ToolsRegistry)

	// Verify tools were discovered
	tools := cfg.ToolsRegistry.ListTools()
	assert.GreaterOrEqual(t, len(tools), 1)
}

// TestNewOrlaConfigFromPath_AbsoluteToolsDir tests loading config with absolute tools directory
func TestNewOrlaConfigFromPath_AbsoluteToolsDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")

	// Create a tools directory
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	err := os.MkdirAll(toolsDir, 0755)
	require.NoError(t, err)

	// Create a test tool
	toolPath := filepath.Join(toolsDir, "test-tool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(toolPath, []byte("#!/bin/sh\necho hello\n"), 0755)
	require.NoError(t, err)

	// Create config file with absolute path
	config := OrlaConfig{
		ToolsDir:  toolsDir, // Absolute path
		Port:      9000,
		Timeout:   60,
		LogFormat: "pretty",
		LogLevel:  "debug",
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := NewOrlaConfigFromPath(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify absolute path is preserved
	assert.Equal(t, toolsDir, cfg.ToolsDir)
	assert.NotNil(t, cfg.ToolsRegistry)
}

// TestNewOrlaConfigFromPath_WithToolsRegistry tests loading config with tools registry defined
func TestNewOrlaConfigFromPath_WithToolsRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")

	// Create config file with tools registry
	toolEntry := &ToolEntry{
		Name:        "test-tool",
		Description: "A test tool",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	registry := NewToolsRegistry()
	err := registry.AddTool(toolEntry)
	require.NoError(t, err)

	config := OrlaConfig{
		ToolsDir:      "", // Empty - should use registry
		ToolsRegistry: registry,
		Port:          9000,
		Timeout:       60,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := NewOrlaConfigFromPath(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify registry is used
	assert.NotNil(t, cfg.ToolsRegistry)
	tool, err := cfg.ToolsRegistry.GetTool("test-tool")
	require.NoError(t, err)
	assert.Equal(t, toolEntry, tool)
}

// TestNewOrlaConfigFromPath_NoToolsDirNoRegistry tests error when neither tools dir nor registry is provided
func TestNewOrlaConfigFromPath_NoToolsDirNoRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")

	// Create config file without tools dir or registry
	config := OrlaConfig{
		ToolsDir:      "",
		ToolsRegistry: nil,
		Port:          9000,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	// Load config should fail
	cfg, err := NewOrlaConfigFromPath(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "tools dir is unset and tools registry is not defined")
}

// TestNewOrlaConfigFromPath_InvalidJSON tests loading an invalid JSON config file
func TestNewOrlaConfigFromPath_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")

	// Create invalid JSON
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(configPath, []byte("{ invalid json }"), 0644)
	require.NoError(t, err)

	// Load config should fail
	cfg, err := NewOrlaConfigFromPath(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

// TestNewOrlaConfigFromPath_NonexistentFile tests loading a non-existent config file
func TestNewOrlaConfigFromPath_NonexistentFile(t *testing.T) {
	cfg, err := NewOrlaConfigFromPath("/nonexistent/path/orla.json")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestNewOrlaConfigFromPath_NonexistentToolsDir tests graceful handling when tools directory doesn't exist
func TestNewOrlaConfigFromPath_NonexistentToolsDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.json")

	// Create config file with non-existent tools directory
	config := OrlaConfig{
		ToolsDir: "./nonexistent-tools",
		Port:     9000,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	// Load config should succeed even if tools directory doesn't exist (graceful degradation)
	cfg, err := NewOrlaConfigFromPath(configPath)
	require.NoError(t, err, "Should succeed even without tools directory")
	assert.NotNil(t, cfg, "Config should be created")
	assert.Empty(t, cfg.ToolsRegistry.ListTools(), "Should have no tools when directory doesn't exist")
}
