package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
	// Note: Installed tools from ~/.orla/tools/ may still be present even if the directory doesn't exist
	// The function gracefully handles missing directory but still scans installed tools
	tools := cfg.ToolsRegistry.ListTools()
	// Verify that the config was created successfully (tools may include installed tools)
	assert.NotNil(t, tools, "Tools registry should be initialized")
}

// TestNewOrlaConfigFromPath_ValidConfig tests loading a valid config file
func TestNewOrlaConfigFromPath_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.yaml")

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

	configYAML, err := yaml.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configYAML, 0644)
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
	configPath := filepath.Join(tmpDir, "orla.yaml")

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

	configYAML, err := yaml.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configYAML, 0644)
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
	configPath := filepath.Join(tmpDir, "orla.yaml")

	// Create config file with tools registry
	toolEntry := &core.ToolEntry{
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

	configYAML, err := yaml.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configYAML, 0644)
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
	configPath := filepath.Join(tmpDir, "orla.yaml")

	// Create config file without tools dir or registry
	config := OrlaConfig{
		ToolsDir:      "",
		ToolsRegistry: nil,
		Port:          9000,
	}

	configYAML, err := yaml.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configYAML, 0644)
	require.NoError(t, err)

	// Load config should fail
	cfg, err := NewOrlaConfigFromPath(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "tools dir is unset and tools registry is not defined")
}

// TestNewOrlaConfigFromPath_InvalidYAML tests loading an invalid YAML config file
func TestNewOrlaConfigFromPath_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.yaml")

	// Create invalid YAML
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err := os.WriteFile(configPath, []byte("invalid: yaml: [unclosed"), 0644)
	require.NoError(t, err)

	// Load config should fail
	cfg, err := NewOrlaConfigFromPath(configPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

// TestNewOrlaConfigFromPath_NonexistentFile tests loading a non-existent config file
func TestNewOrlaConfigFromPath_NonexistentFile(t *testing.T) {
	cfg, err := NewOrlaConfigFromPath("/nonexistent/path/orla.yaml")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestNewOrlaConfigFromPath_NonexistentToolsDir tests graceful handling when tools directory doesn't exist
func TestNewOrlaConfigFromPath_NonexistentToolsDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.yaml")

	// Create config file with non-existent tools directory
	config := OrlaConfig{
		ToolsDir: "./nonexistent-tools",
		Port:     9000,
	}

	configYAML, err := yaml.Marshal(config)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	err = os.WriteFile(configPath, configYAML, 0644)
	require.NoError(t, err)

	// Load config should succeed even if tools directory doesn't exist (graceful degradation)
	cfg, err := NewOrlaConfigFromPath(configPath)
	require.NoError(t, err, "Should succeed even without tools directory")
	assert.NotNil(t, cfg, "Config should be created")
	// Note: Installed tools from ~/.orla/tools/ may still be present even if the directory doesn't exist
	// The function gracefully handles missing directory but still scans installed tools
	tools := cfg.ToolsRegistry.ListTools()
	// Verify that the config was created successfully (tools may include installed tools)
	assert.NotNil(t, tools, "Tools registry should be initialized")
}

// TestSetToolsDir tests setting the tools directory
func TestSetToolsDir(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create a test executable
	testTool := filepath.Join(toolsDir, "test-tool")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(testTool, []byte("#!/bin/sh\necho test"), 0755))

	cfg := &OrlaConfig{
		Port:      8080,
		Timeout:   30,
		LogFormat: "json",
		LogLevel:  "info",
	}

	// Set tools directory
	err := cfg.SetToolsDir(toolsDir)
	require.NoError(t, err)
	assert.Equal(t, toolsDir, cfg.ToolsDir)
	assert.NotNil(t, cfg.ToolsRegistry)
	assert.NotEmpty(t, cfg.ToolsRegistry.Tools)
}

// TestSetToolsDir_EmptyString tests that setting an empty tools directory returns an error
func TestSetToolsDir_EmptyString(t *testing.T) {
	cfg := &OrlaConfig{
		Port:      8080,
		Timeout:   30,
		LogFormat: "json",
		LogLevel:  "info",
	}

	err := cfg.SetToolsDir("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// TestSetToolsDir_RelativePath tests that relative paths are resolved to absolute paths
func TestSetToolsDir_RelativePath(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	// Change to temp directory
	require.NoError(t, os.Chdir(tmpDir))

	cfg := &OrlaConfig{
		Port:      8080,
		Timeout:   30,
		LogFormat: "json",
		LogLevel:  "info",
	}

	// Set tools directory with relative path
	err = cfg.SetToolsDir("tools")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(cfg.ToolsDir), "ToolsDir should be absolute")
	// Use filepath.EvalSymlinks to normalize paths (handles /private/var on macOS)
	expectedPath, err := filepath.EvalSymlinks(toolsDir)
	require.NoError(t, err)
	actualPath, err := filepath.EvalSymlinks(cfg.ToolsDir)
	require.NoError(t, err)
	assert.Equal(t, expectedPath, actualPath)
}

// TestValidateConfig tests validation of configuration values
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *OrlaConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: false,
		},
		{
			name: "invalid port - negative",
			config: &OrlaConfig{
				Port:      -1,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: true,
			errMsg:  "port must be between 0 and 65535",
		},
		{
			name: "invalid port - too large",
			config: &OrlaConfig{
				Port:      70000,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: true,
			errMsg:  "port must be between 0 and 65535",
		},
		{
			name: "invalid timeout - zero",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   0,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: false, // Timeout defaults to 30 if 0
		},
		{
			name: "invalid timeout - negative",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   -1,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: true,
			errMsg:  "timeout must be at least 1 second",
		},
		{
			name: "invalid log format",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   30,
				LogFormat: "invalid",
				LogLevel:  "info",
			},
			wantErr: true,
			errMsg:  "log_format must be 'json' or 'pretty'",
		},
		{
			name: "invalid log level",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "invalid",
			},
			wantErr: true,
			errMsg:  "log_level must be one of",
		},
		{
			name: "valid log format - pretty",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   30,
				LogFormat: "pretty",
				LogLevel:  "info",
			},
			wantErr: false,
		},
		{
			name: "valid log levels",
			config: &OrlaConfig{
				Port:      8080,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "debug",
			},
			wantErr: false,
		},
		{
			name: "port 0 is valid",
			config: &OrlaConfig{
				Port:      0,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: false,
		},
		{
			name: "port 65535 is valid",
			config: &OrlaConfig{
				Port:      65535,
				Timeout:   30,
				LogFormat: "json",
				LogLevel:  "info",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRebuildToolsRegistry_ScanToolsError tests rebuildToolsRegistry when ScanToolsFromDirectory fails
func TestRebuildToolsRegistry_ScanToolsError(t *testing.T) {
	cfg := &OrlaConfig{
		ToolsDir: "/nonexistent/path/that/causes/error", // Path that causes error
		Port:     8080,
		Timeout:  30,
	}

	// This should fail because the path doesn't exist and might cause an error
	// Actually, ScanToolsFromDirectory returns empty map for non-existent dir, not error
	// So we need a different approach - use a file instead of directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "notadir")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0644))

	cfg.ToolsDir = filePath
	err := cfg.rebuildToolsRegistry()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// TestRebuildToolsRegistry_InstalledToolsError tests rebuildToolsRegistry when getInstalledToolsDir fails
// This is hard to test directly, but we can test the merge logic
func TestRebuildToolsRegistry_MergesInstalledTools(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create a tool in directory
	tool1 := filepath.Join(toolsDir, "dir-tool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(tool1, []byte("#!/bin/sh"), 0755))

	cfg := &OrlaConfig{
		ToolsDir: toolsDir,
		Port:     8080,
		Timeout:  30,
	}

	err := cfg.rebuildToolsRegistry()
	require.NoError(t, err)
	assert.NotNil(t, cfg.ToolsRegistry)

	// Should have at least the directory tool (name is without extension)
	tools := cfg.ToolsRegistry.ListTools()
	assert.GreaterOrEqual(t, len(tools), 1)

	// Check if dir-tool exists (name without .sh extension)
	found := false
	for _, tool := range tools {
		if tool.Name == "dir-tool" {
			found = true
			break
		}
	}
	assert.True(t, found, "dir-tool should be found in tools list")
}

// TestNewDefaultOrlaConfig_AbsPathError tests NewDefaultOrlaConfig when filepath.Abs fails
// This is hard to test, but we can test the error path exists
func TestNewDefaultOrlaConfig_WithToolsDirError(t *testing.T) {
	// Create a scenario where rebuildToolsRegistry might fail
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if restoreErr := os.Chdir(originalDir); restoreErr != nil {
			t.Logf("Failed to restore working directory: %v", restoreErr)
		}
	}()

	// Change to temp directory
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	// Create a file named "tools" (not a directory) in current directory
	// This will cause ScanToolsFromDirectory to fail
	toolsFile := filepath.Join(tmpDir, "tools")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(toolsFile, []byte("not a directory"), 0644))

	cfg, err := NewDefaultOrlaConfig()
	// Should fail because tools is a file, not a directory
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to create tools registry")
}
