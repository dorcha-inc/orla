package tool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestInstallTool_SuccessMessage(t *testing.T) {
	// Test that success messages are written after successful installation
	// Note: Full installation testing is done in installer package tests.
	// This test verifies the wrapper writes success messages.
	// We test the local path case which is simpler and doesn't require registry setup.
	tmpDir := t.TempDir()
	localToolDir := filepath.Join(tmpDir, "local-tool")
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(localToolDir, 0755))

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
	require.NoError(t, os.WriteFile(filepath.Join(localToolDir, installer.ToolManifestFileName), manifestData, 0644))

	// Create entrypoint
	entrypointPath := filepath.Join(localToolDir, "bin", "tool")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho 'test tool'"), 0755))

	var buf bytes.Buffer
	err = InstallTool("", InstallOptions{
		LocalPath: localToolDir,
		Writer:    &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	// Local installation doesn't print "Successfully installed" message
	// but does print the restart message
	assert.Contains(t, output, "Tool is now available. Restart orla server to use it.")
}

func TestInstallTool_LocalPath(t *testing.T) {
	// Set up temporary directories
	tmpDir := t.TempDir()
	localToolDir := filepath.Join(tmpDir, "local-tool")
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(localToolDir, 0755))

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

	// Create tool.yaml manifest
	manifest := &core.ToolManifest{
		Name:        "local-test-tool",
		Version:     "0.1.0",
		Description: "A local test tool",
		Entrypoint:  "bin/tool",
	}

	manifestData, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(localToolDir, installer.ToolManifestFileName), manifestData, 0644))

	// Create entrypoint
	entrypointPath := filepath.Join(localToolDir, "bin", "tool")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho 'local tool'"), 0755))

	var buf bytes.Buffer
	err = InstallTool("", InstallOptions{
		LocalPath: localToolDir,
		Writer:    &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Tool is now available. Restart orla server to use it.")
}

func TestInstallTool_LocalPath_Error(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)
	require.NoError(t, os.Chdir(tmpDir))

	var buf bytes.Buffer
	err = InstallTool("", InstallOptions{
		LocalPath: "/nonexistent/path",
		Writer:    &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install local tool")
}

func TestInstallTool_RegistryInstall_Error(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)
	require.NoError(t, os.Chdir(tmpDir))

	var buf bytes.Buffer
	err = InstallTool("nonexistent-tool", InstallOptions{
		RegistryURL: registry.DefaultRegistryURL,
		Version:     "latest",
		Writer:      &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install tool")
}

func TestInstallTool_EmptyToolName_WithLocalPath(t *testing.T) {
	// Test that empty tool name is allowed when using local path
	tmpDir := t.TempDir()
	localToolDir := filepath.Join(tmpDir, "local-tool")
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(localToolDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)
	require.NoError(t, os.Chdir(tmpDir))

	// Create tool.yaml manifest
	manifest := &core.ToolManifest{
		Name:        "local-tool",
		Version:     "0.1.0",
		Description: "A local test tool",
		Entrypoint:  "bin/tool",
	}

	manifestData, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(localToolDir, installer.ToolManifestFileName), manifestData, 0644))

	// Create entrypoint
	entrypointPath := filepath.Join(localToolDir, "bin", "tool")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho 'local tool'"), 0755))

	var buf bytes.Buffer
	err = InstallTool("", InstallOptions{
		LocalPath: localToolDir,
		Writer:    &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Tool is now available. Restart orla server to use it.")
}

func TestInstallTool_LocalPathTakesPrecedence(t *testing.T) {
	// Test that when LocalPath is provided, registry URL and version are ignored
	tmpDir := t.TempDir()
	localToolDir := filepath.Join(tmpDir, "local-tool")
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(localToolDir, 0755))

	// Create orla.yaml config
	configPath := filepath.Join(tmpDir, "orla.yaml")
	configContent := fmt.Sprintf("tools_dir: %s\n", installDir)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Change to temp directory
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer core.LogDeferredError1(os.Chdir, originalDir)
	require.NoError(t, os.Chdir(tmpDir))

	// Create tool.yaml manifest
	manifest := &core.ToolManifest{
		Name:        "local-tool",
		Version:     "0.1.0",
		Description: "A local test tool",
		Entrypoint:  "bin/tool",
	}

	manifestData, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(localToolDir, installer.ToolManifestFileName), manifestData, 0644))

	// Create entrypoint
	entrypointPath := filepath.Join(localToolDir, "bin", "tool")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho 'local tool'"), 0755))

	var buf bytes.Buffer
	// Provide both LocalPath and RegistryURL - LocalPath should take precedence
	err = InstallTool("some-tool", InstallOptions{
		LocalPath:   localToolDir,
		RegistryURL: "https://example.com/registry",
		Version:     "1.0.0",
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	// Should use local installation, not registry
	assert.Contains(t, output, "Tool is now available. Restart orla server to use it.")
	// Should not contain "Successfully installed" which is only for registry installs
	assert.NotContains(t, output, "Successfully installed")
}
