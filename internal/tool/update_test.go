package tool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestUpdateTool_Success(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create tool directory structure (simulating an installed tool)
	toolDir := filepath.Join(installDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	// Create tool.yaml manifest
	manifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
	}
	manifestData, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	// Create orla.yaml config to use project-local tools directory
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory so config.LoadConfig finds orla.yaml
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Logf("Failed to restore working directory: %v", chdirErr)
		}
	}()
	require.NoError(t, os.Chdir(tmpDir))
}

func TestUpdateTool_NotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory so config.LoadConfig finds orla.yaml
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Logf("Failed to restore working directory: %v", chdirErr)
		}
	}()
	require.NoError(t, os.Chdir(tmpDir))

	// Try to update non-existent tool
	var buf bytes.Buffer
	err = UpdateTool("nonexistent-tool", UpdateOptions{
		Writer: &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update tool")
	assert.Contains(t, err.Error(), "not installed")
}

func TestUpdateTool_DefaultWriter(t *testing.T) {
	// Test that UpdateTool uses os.Stdout when Writer is nil
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory so config.LoadConfig finds orla.yaml
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Logf("Failed to restore working directory: %v", chdirErr)
		}
	}()
	require.NoError(t, os.Chdir(tmpDir))

	// Try to update non-existent tool with nil Writer (should use os.Stdout)
	err = UpdateTool("nonexistent-tool", UpdateOptions{
		Writer: nil, // Should default to os.Stdout
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update tool")
}

func TestUpdateTool_CustomRegistryURL(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create tool directory structure
	toolDir := filepath.Join(installDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory so config.LoadConfig finds orla.yaml
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Logf("Failed to restore working directory: %v", chdirErr)
		}
	}()
	require.NoError(t, os.Chdir(tmpDir))

	// Test with custom registry URL
	var buf bytes.Buffer
	err = UpdateTool("test-tool", UpdateOptions{
		RegistryURL: "https://custom-registry.example.com",
		Writer:      &buf,
	})
	// This will fail because the registry doesn't exist, but we're testing
	// that the custom URL is passed through
	require.Error(t, err)
	// The error should be from the installer, not from config loading
	assert.NotContains(t, err.Error(), "tools directory not configured")
}
