package tool

import (
	"bytes"
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
	localToolDir := t.TempDir()
	installDir := t.TempDir()

	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

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
	localToolDir := t.TempDir()
	installDir := t.TempDir()

	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

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
	installDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := InstallTool("", InstallOptions{
		LocalPath: "/nonexistent/path",
		Writer:    &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install local tool")
}

func TestInstallTool_RegistryInstall_Error(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := InstallTool("nonexistent-tool", InstallOptions{
		RegistryURL: registry.DefaultRegistryURL,
		Version:     "latest",
		Writer:      &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install tool")
}

func TestInstallTool_EmptyToolName_WithLocalPath(t *testing.T) {
	// Test that empty tool name is allowed when using local path
	localToolDir := t.TempDir()
	installDir := t.TempDir()

	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

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
	localToolDir := t.TempDir()
	installDir := t.TempDir()

	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

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
