package tool

import (
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

func TestUninstallTool_Success(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create tool directory structure
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
	defer core.LogDeferredError1(os.Chdir, originalDir)
	require.NoError(t, os.Chdir(tmpDir))

	// Uninstall tool
	err = UninstallTool("test-tool")
	require.NoError(t, err)

	// Verify tool directory is removed
	_, err = os.Stat(toolDir)
	assert.True(t, os.IsNotExist(err))
}

func TestUninstallTool_NotInstalled(t *testing.T) {
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
	defer core.LogDeferredError1(os.Chdir, originalDir)
	require.NoError(t, os.Chdir(tmpDir))

	// Try to uninstall non-existent tool
	err = UninstallTool("nonexistent-tool")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall tool")
	assert.Contains(t, err.Error(), "not installed")
}
