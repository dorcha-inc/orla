package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/dorcha-inc/orla/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const invalidValue = "invalid"

// setupTestConfig creates a temporary directory with an orla.yaml config file
// and changes to that directory. Returns the temp directory and a cleanup function.
func setupTestConfig(t *testing.T) (tmpDir string, cleanup func()) {
	tmpDir = t.TempDir()

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := "tools_dir: ./.orla/tools\nport: 8080\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	cleanup = func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			// Can't use t.Logf in cleanup, so we ignore the error
			_ = chdirErr
		}
	}
	require.NoError(t, os.Chdir(tmpDir))

	return tmpDir, cleanup
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Change to a temp directory to ensure no project config exists
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	defer core.LogDeferredError(func() error { return os.Chdir(originalDir) })

	// Load config without any config files (should use defaults)
	cfg, loadConfigErr := LoadConfig("")
	require.NoError(t, loadConfigErr)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, 30, cfg.Timeout)
	// Note: LogFormat and LogLevel are empty strings by default in struct, but validateConfig sets defaults
	// After validation, they should have defaults
	assert.Equal(t, DefaultModel, cfg.Model)
	assert.Equal(t, 10, cfg.MaxToolCalls)
	// Note: Streaming defaults to true in Viper, but struct default is false
	// After unmarshaling, it should be true
	assert.Equal(t, OrlaOutputFormatAuto, cfg.OutputFormat)
	// Note: ConfirmDestructive defaults to true in Viper, but struct default is false
	// After unmarshaling, it should be true
	assert.Equal(t, false, cfg.DryRun)

	// Without project config, tools_dir should default to ~/.orla/tools
	// (not ./orla/tools relative to temp directory)
	orlaHome, err := registry.GetOrlaHomeDir()
	require.NoError(t, err)
	expectedToolsDir := filepath.Join(orlaHome, "tools")
	// Use EvalSymlinks to handle any symlink differences
	expectedAbs, evalSymlinksErr := filepath.EvalSymlinks(expectedToolsDir)
	require.NoError(t, evalSymlinksErr)
	actualAbs, evalSymlinksErr2 := filepath.EvalSymlinks(cfg.ToolsDir)
	require.NoError(t, evalSymlinksErr2)
	assert.Equal(t, expectedAbs, actualAbs)
}

func TestLoadConfig_WithProjectConfig(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	cfg, err := LoadConfig("")
	require.NoError(t, err)

	// Should use project config values
	assert.NotEmpty(t, cfg.ToolsDir)
	assert.Equal(t, 8080, cfg.Port)
}

func TestLoadConfig_WithSpecificPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom.yaml")
	configContent := "port: 9000\ntimeout: 60\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 60, cfg.Timeout)
}

func TestLoadConfig_ProjectConfigPrecedence(t *testing.T) {
	tmpDir, cleanup := setupTestConfig(t)
	defer cleanup()

	// Create user config
	orlaHome, err := registry.GetOrlaHomeDir()
	require.NoError(t, err)
	userConfigDir := filepath.Join(orlaHome, "config.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(userConfigDir), 0755))
	userConfigContent := "port: 7000\ntimeout: 45\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(userConfigDir, []byte(userConfigContent), 0644))
	defer core.LogDeferredError(func() error { return os.Remove(userConfigDir) })

	// Update project config
	projectConfigPath := filepath.Join(tmpDir, "orla.yaml")
	projectConfigContent := "port: 9000\ntimeout: 60\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(projectConfigPath, []byte(projectConfigContent), 0644))

	cfg, err := LoadConfig("")
	require.NoError(t, err)

	// Project config should override user config
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 60, cfg.Timeout)
}

func TestLoadConfig_EnvironmentVariableOverride(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Set environment variable
	t.Setenv("ORLA_PORT", "5000")

	cfg, loadConfigErr := LoadConfig("")
	require.NoError(t, loadConfigErr)

	// Environment variable should override config file
	assert.Equal(t, 5000, cfg.Port)
}

func TestLoadConfig_InvalidConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte("invalid: yaml: content: [unclosed"), 0644))

	_, err := LoadConfig(configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestSetToolsDir(t *testing.T) {
	cfg := &OrlaConfig{}

	// Test with absolute path
	tmpDir := t.TempDir()
	err := cfg.SetToolsDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, tmpDir, cfg.ToolsDir)

	// Test with relative path
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	cfg2 := &OrlaConfig{}
	err = cfg2.SetToolsDir("./relative-tools")
	require.NoError(t, err)
	assert.NotEmpty(t, cfg2.ToolsDir)
	assert.True(t, filepath.IsAbs(cfg2.ToolsDir))
}

func TestSetToolsDir_Empty(t *testing.T) {
	cfg := &OrlaConfig{}
	err := cfg.SetToolsDir("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestGetConfigValue(t *testing.T) {
	tmpDir, cleanup := setupTestConfig(t)
	defer cleanup()

	// Update config with custom values
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := "port: 9000\nmodel: openai:gpt-4\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Get port value
	portVal, err := GetConfigValue("port")
	require.NoError(t, err)
	assert.Equal(t, 9000, portVal.Value)
	assert.Equal(t, "project", portVal.Source)

	// Get model value
	modelVal, err := GetConfigValue("model")
	require.NoError(t, err)
	assert.Equal(t, "openai:gpt-4", modelVal.Value)
	assert.Equal(t, "project", modelVal.Source)

	// Get default value
	timeoutVal, err := GetConfigValue("timeout")
	require.NoError(t, err)
	assert.Equal(t, 30, timeoutVal.Value)
	assert.Equal(t, "default", timeoutVal.Source)
}

func TestGetConfigValue_UnknownKey(t *testing.T) {
	_, err := GetConfigValue("unknown_key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown config key")
}

func TestGetConfigValue_EnvironmentVariable(t *testing.T) {
	t.Setenv("ORLA_PORT", "7777")

	portVal, err := GetConfigValue("port")
	require.NoError(t, err)
	assert.Equal(t, "7777", portVal.Value)
	assert.Equal(t, "env", portVal.Source)
}

func TestSetConfigValue_ProjectConfig(t *testing.T) {
	tmpDir, cleanup := setupTestConfig(t)
	defer cleanup()

	// Set a value
	err := SetConfigValue("port", "9999")
	require.NoError(t, err)

	// Verify it was saved
	cfg, loadConfigErr := LoadConfig("")
	require.NoError(t, loadConfigErr)
	assert.Equal(t, 9999, cfg.Port)

	// Verify file was updated
	configPath := filepath.Join(tmpDir, "orla.yaml")
	// #nosec G304 -- test file inclusion via variable is acceptable for test files
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	data, readFileErr := os.ReadFile(configPath)
	require.NoError(t, readFileErr)
	assert.Contains(t, string(data), "port: 9999")
}

func TestSetConfigValue_UserConfig(t *testing.T) {
	// Remove project config if it exists
	projectPath, err := GetProjectConfigPath()
	require.NoError(t, err)
	if removeErr := os.Remove(projectPath); removeErr != nil && !os.IsNotExist(removeErr) {
		require.NoError(t, removeErr)
	}

	// Get user config path and create an empty file first (since setupViper now requires file to exist)
	userPath, err := GetUserConfigPath()
	require.NoError(t, err)

	// Ensure directory exists
	configDir := filepath.Dir(userPath)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Create empty config file (setupViper requires file to exist)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(userPath, []byte(""), 0644))
	defer func() {
		if removeErr := os.Remove(userPath); removeErr != nil && !os.IsNotExist(removeErr) {
			// Can't use t.Logf in cleanup, so we ignore the error
			_ = removeErr
		}
	}()

	// Set a value (should update user config)
	err = SetConfigValue("port", "8888")
	require.NoError(t, err)

	// Verify it was saved to user config
	// #nosec G304 -- test file inclusion via variable is acceptable for test files
	data, err := os.ReadFile(userPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "port: 8888")
}

func TestListConfig(t *testing.T) {
	tmpDir, cleanup := setupTestConfig(t)
	defer cleanup()

	// Update config with some values
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := "port: 9000\nmodel: openai:gpt-4\ntimeout: 60\n"
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	configMap, err := ListConfig()
	require.NoError(t, err)

	// Check that we have some config values
	assert.NotEmpty(t, configMap)

	// Check specific values
	portVal, ok := configMap["port"]
	require.True(t, ok)
	assert.Equal(t, 9000, portVal.Value)
	assert.Equal(t, "project", portVal.Source)

	modelVal, ok := configMap["model"]
	require.True(t, ok)
	assert.Equal(t, "openai:gpt-4", modelVal.Value)
	assert.Equal(t, "project", modelVal.Source)
}

func TestValidateConfig(t *testing.T) {
	cfg := &OrlaConfig{
		Port:         8080,
		Timeout:      30,
		Model:        DefaultModel,
		MaxToolCalls: DefaultMaxToolCalls,
		OutputFormat: OrlaOutputFormatAuto,
	}

	err := validateConfig(cfg)
	require.NoError(t, err)

	// Test invalid port
	cfg.Port = 70000
	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between 0 and 65535")

	// Test invalid timeout
	cfg.Port = 8080
	cfg.Timeout = 0
	// validateConfig sets default timeout to 30 if 0, so we need to set it to -1 to trigger error
	cfg.Timeout = -1
	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be at least 1 second")

	// Test invalid log format
	cfg.Timeout = 30
	cfg.LogFormat = invalidValue
	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log_format must be one of")

	// Test invalid log level
	cfg.LogFormat = "json"
	cfg.LogLevel = invalidValue
	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "log_level must be one of")

	// Test invalid max_tool_calls
	cfg.LogLevel = "info"
	cfg.MaxToolCalls = -1
	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_tool_calls must be at least 1")

	// Test invalid output_format
	cfg.MaxToolCalls = 10
	cfg.OutputFormat = invalidValue
	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_format must be one of")
}

func TestPostProcessConfig_NoProjectConfig(t *testing.T) {
	// Ensure no project config exists
	projectPath, err := GetProjectConfigPath()
	require.NoError(t, err)
	if removeErr := os.Remove(projectPath); removeErr != nil && !os.IsNotExist(removeErr) {
		require.NoError(t, removeErr)
	}

	cfg := &OrlaConfig{}
	err = postProcessConfig(cfg, "")
	require.NoError(t, err)

	// Should default to ~/.orla/tools
	orlaHome, err := registry.GetOrlaHomeDir()
	require.NoError(t, err)
	expectedToolsDir := filepath.Join(orlaHome, "tools")
	assert.Equal(t, expectedToolsDir, cfg.ToolsDir)
}

func TestPostProcessConfig_WithProjectConfig(t *testing.T) {
	tmpDir, cleanup := setupTestConfig(t)
	defer cleanup()

	cfg := &OrlaConfig{}
	configFileDir := tmpDir
	err := postProcessConfig(cfg, configFileDir)
	require.NoError(t, err)

	// When tools_dir is not set, should use global ~/.orla/tools
	orlaHome, err := registry.GetOrlaHomeDir()
	require.NoError(t, err)
	expectedToolsDir := filepath.Join(orlaHome, "tools")
	assert.Equal(t, expectedToolsDir, cfg.ToolsDir)
}

func TestPostProcessConfig_ToolsRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	toolPath := filepath.Join(tmpDir, "tool1")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolPath, 0755))

	// Create a simple tool executable
	toolExec := filepath.Join(toolPath, "tool1")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(toolExec, []byte("#!/bin/sh\necho test\n"), 0755))

	cfg := &OrlaConfig{
		ToolsDir: toolPath,
	}

	err := postProcessConfig(cfg, "")

	// Should not error
	require.NoError(t, err)
	assert.NotNil(t, cfg.ToolsRegistry)
}

func TestPostProcessConfig_ToolsRegistry_NoConfigFileDir(t *testing.T) {
	// Test the error case when ToolsRegistry is set but configFileDir is empty
	cfg := &OrlaConfig{
		ToolsRegistry: &state.ToolsRegistry{
			Tools: map[string]*core.ToolManifest{
				"tool1": {
					Name: "tool1",
					Path: "tool1",
				},
			},
		},
	}

	err := postProcessConfig(cfg, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file directory is not set but ToolsRegistry is set")
}

func TestPostProcessConfig_ToolsRegistry_WithConfigFileDir(t *testing.T) {
	// Test the success case when ToolsRegistry is set and configFileDir is provided
	tmpDir := t.TempDir()

	// Create a tool file in the config directory
	toolPath := filepath.Join(tmpDir, "tool1")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(toolPath, []byte("#!/bin/sh\necho test\n"), 0755))

	cfg := &OrlaConfig{
		ToolsRegistry: &state.ToolsRegistry{
			Tools: map[string]*core.ToolManifest{
				"tool1": {
					Name: "tool1",
					Path: "tool1", // Relative path should be resolved
				},
			},
		},
	}

	// configFileDir should be the directory containing the config file, not the file itself
	configFileDir := tmpDir

	err := postProcessConfig(cfg, configFileDir)
	require.NoError(t, err)

	// Verify that the relative path was resolved to an absolute path
	assert.NotNil(t, cfg.ToolsRegistry)
	assert.NotNil(t, cfg.ToolsRegistry.Tools["tool1"])
	assert.True(t, filepath.IsAbs(cfg.ToolsRegistry.Tools["tool1"].Path), "Tool path should be resolved to absolute path")
	assert.Equal(t, toolPath, cfg.ToolsRegistry.Tools["tool1"].Path, "Tool path should be resolved correctly")

	// Let's test when the tool path is empty and the entrypoint is set
	tools := cfg.ToolsRegistry.Tools
	tools["tool2"] = &core.ToolManifest{
		Name:       "tool2",
		Entrypoint: "tool2",
	}
	err = postProcessConfig(cfg, configFileDir)
	require.NoError(t, err)
	assert.NotNil(t, cfg.ToolsRegistry)
	assert.NotNil(t, cfg.ToolsRegistry.Tools["tool2"])
}

func TestGetUserConfigPath(t *testing.T) {
	path, err := GetUserConfigPath()
	require.NoError(t, err)

	orlaHome, err := registry.GetOrlaHomeDir()
	require.NoError(t, err)
	expected := filepath.Join(orlaHome, "config.yaml")
	assert.Equal(t, expected, path)
}

func TestGetProjectConfigPath(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError(func() error { return os.Chdir(originalDir) })

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	path, err := GetProjectConfigPath()
	require.NoError(t, err)

	// Use filepath.EvalSymlinks to handle /var -> /private/var symlink on macOS
	// Evaluate symlinks on the directory part since the file may not exist
	expectedDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	expectedAbs := filepath.Join(expectedDir, "orla.yaml")
	pathDir := filepath.Dir(path)
	pathDirAbs, err := filepath.EvalSymlinks(pathDir)
	require.NoError(t, err)
	pathAbs := filepath.Join(pathDirAbs, filepath.Base(path))
	assert.Equal(t, expectedAbs, pathAbs)
}

func TestOrlaConfig_SetToolsDir_RelativePath(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError(func() error { return os.Chdir(originalDir) })

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	cfg := &OrlaConfig{}
	err = cfg.SetToolsDir("./relative-tools")
	require.NoError(t, err)

	// Should be resolved to absolute path
	assert.True(t, filepath.IsAbs(cfg.ToolsDir))
	assert.Contains(t, cfg.ToolsDir, "relative-tools")
}

func TestLoadConfig_WithYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.yaml")
	configContent := `
port: 9000
timeout: 60
log_format: pretty
log_level: debug
model: openai:gpt-4
max_tool_calls: 20
streaming: false
output_format: rich
confirm_destructive: false
dry_run: true
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 60, cfg.Timeout)
	assert.Equal(t, OrlaLogFormatPretty, cfg.LogFormat)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "openai:gpt-4", cfg.Model)
	assert.Equal(t, 20, cfg.MaxToolCalls)
	assert.Equal(t, false, cfg.Streaming)
	assert.Equal(t, OrlaOutputFormatRich, cfg.OutputFormat)
	assert.Equal(t, false, cfg.ConfirmDestructive)
	assert.Equal(t, true, cfg.DryRun)
}

func TestSetConfigValue_ComplexValue(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Set a string value
	err := SetConfigValue("model", "ollama:llama2:7b")
	require.NoError(t, err)

	// Set an integer value (as string)
	err = SetConfigValue("port", "7777")
	require.NoError(t, err)

	// Verify both were saved
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	assert.Equal(t, "ollama:llama2:7b", cfg.Model)
	assert.Equal(t, 7777, cfg.Port)
}

func TestListConfig_AllDefaults(t *testing.T) {
	// Ensure no project config exists
	projectPath, err := GetProjectConfigPath()
	require.NoError(t, err)
	if removeErr := os.Remove(projectPath); removeErr != nil && !os.IsNotExist(removeErr) {
		require.NoError(t, removeErr)
	}

	// Load config with no files (all defaults)
	configMap, err := ListConfig()
	require.NoError(t, err)

	// Should have all default values
	assert.NotEmpty(t, configMap)

	// Check some defaults
	portVal, ok := configMap["port"]
	require.True(t, ok)
	assert.Equal(t, 8080, portVal.Value)
	assert.Equal(t, "default", portVal.Source)

	modelVal, ok := configMap["model"]
	require.True(t, ok)
	assert.Equal(t, DefaultModel, modelVal.Value)
	assert.Equal(t, "default", modelVal.Source)
}

func TestConfigValue_Source(t *testing.T) {
	// Test that source is correctly identified
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Set environment variable
	t.Setenv("ORLA_TIMEOUT", "99")

	// Get timeout (should come from env)
	// Note: environment variables are strings, but Viper converts them to the expected type
	timeoutVal, err := GetConfigValue("timeout")
	require.NoError(t, err)
	// Environment variable is set as string "99", but Viper should convert it to int
	// The actual value might be string or int depending on how Viper handles it
	assert.NotNil(t, timeoutVal.Value)
	assert.Equal(t, "env", timeoutVal.Source)

	// Get port (should come from project config)
	portVal, err := GetConfigValue("port")
	require.NoError(t, err)
	assert.Equal(t, 8080, portVal.Value)
	assert.Equal(t, "project", portVal.Source)
}

func TestSetConfigValue_PreservesOtherFields(t *testing.T) {
	_, cleanup := setupTestConfig(t)
	defer cleanup()

	// Load existing config
	cfg1, err := LoadConfig("")
	require.NoError(t, err)
	originalPort := cfg1.Port

	// Set a different field
	err = SetConfigValue("model", "openai:gpt-4")
	require.NoError(t, err)

	// Reload and verify port is preserved
	cfg2, err := LoadConfig("")
	require.NoError(t, err)
	assert.Equal(t, originalPort, cfg2.Port)
	assert.Equal(t, "openai:gpt-4", cfg2.Model)
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte("invalid: yaml: [unclosed"), 0644))

	_, err := LoadConfig(configPath)
	require.Error(t, err)
}

func TestRebuildToolsRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	cfg := &OrlaConfig{
		ToolsDir: toolsDir,
	}

	err := cfg.rebuildToolsRegistry()
	// Should not error even with empty tools directory
	require.NoError(t, err)
	assert.NotNil(t, cfg.ToolsRegistry)
}

func TestValidateConfig_Defaults(t *testing.T) {
	// Test that validateConfig errors on empty/zero values when called directly
	// (since Viper isn't configured, it can't distinguish between unset and explicitly empty)
	cfg := &OrlaConfig{}
	err := validateConfig(cfg)
	require.Error(t, err)

	// When called through LoadConfig, Viper applies defaults during Unmarshal,
	// so empty values only occur if explicitly set in config files (which should error)
	// Test this by loading a config with missing fields (should use defaults)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := `port: 9000
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer core.LogDeferredError(func() error { return os.Chdir(originalDir) })

	cfg2, err := LoadConfig("")
	require.NoError(t, err)

	// Should have defaults for fields not in config file
	assert.Equal(t, 30, cfg2.Timeout)
	assert.Equal(t, DefaultModel, cfg2.Model)
	assert.Equal(t, DefaultMaxToolCalls, cfg2.MaxToolCalls)
	assert.Equal(t, OrlaOutputFormatAuto, cfg2.OutputFormat)
	assert.Equal(t, 9000, cfg2.Port) // Explicitly set value
}

func TestValidateConfig_BadValues(t *testing.T) {
	cfg := &OrlaConfig{
		Port: 70000,
	}

	err := validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between 0 and 65535")

	cfg = &OrlaConfig{
		Model:        "test",
		MaxToolCalls: 10,
		Timeout:      0,
		OutputFormat: "auto",
	}

	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be at least 1 second")

	cfg.Timeout = 30
	cfg.Model = ""

	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model cannot be empty")

	cfg.Model = "test"
	cfg.MaxToolCalls = 0

	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_tool_calls must be at least 1")

	cfg.MaxToolCalls = 10
	cfg.OutputFormat = "invalid"

	err = validateConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_format must be one of")
}

func TestConfig_MarshalUnmarshal(t *testing.T) {
	cfg := &OrlaConfig{
		Port:    9000,
		Timeout: 60,
		Model:   "openai:gpt-4",
	}

	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)

	var cfg2 OrlaConfig
	err = yaml.Unmarshal(data, &cfg2)
	require.NoError(t, err)

	assert.Equal(t, cfg.Port, cfg2.Port)
	assert.Equal(t, cfg.Timeout, cfg2.Timeout)
	assert.Equal(t, cfg.Model, cfg2.Model)
}

// TestLoadConfig_ExplicitEmptyValues tests that explicit empty/zero values in config files
// correctly raise validation errors (they should not silently use defaults)
func TestLoadConfig_ExplicitEmptyValues(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "orla.yaml")

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer core.LogDeferredError(func() error { return os.Chdir(originalDir) })

	// Test 1: Explicit empty model should error
	configContent := `model: ""
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))
	_, err = LoadConfig("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model cannot be empty")

	// Test 2: Explicit zero max_tool_calls should error
	configContent = `max_tool_calls: 0
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))
	_, err = LoadConfig("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_tool_calls must be at least 1")

	// Test 3: Missing values (not explicitly set) should use defaults
	configContent = `port: 9000
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))
	cfg, err := LoadConfig("")
	require.NoError(t, err)
	// Model and max_tool_calls should have defaults (not explicitly set to empty)
	assert.Equal(t, DefaultModel, cfg.Model)
	assert.Equal(t, DefaultMaxToolCalls, cfg.MaxToolCalls)
	assert.Equal(t, 9000, cfg.Port) // Explicitly set value should be used
}
