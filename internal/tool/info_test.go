package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGetToolInfo_NotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := GetToolInfo("nonexistent-tool", InfoOptions{
		Writer: &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not installed")
}

func TestGetToolInfo_DefaultWriter(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Test that nil writer defaults to os.Stdout
	err := GetToolInfo("nonexistent-tool", InfoOptions{
		Writer: nil, // Should default to os.Stdout
	})
	// We expect an error, but the important thing is it didn't panic
	assert.Error(t, err)
}

func TestGetToolInfo_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool structure: TOOL-NAME/VERSION/tool.yaml
	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Name:        test-tool")
	assert.Contains(t, output, "Version:     1.0.0")
	assert.Contains(t, output, "Description: A test tool")
	assert.Contains(t, output, "Entrypoint:  bin/tool")
	assert.Contains(t, output, "Path:")
}

func TestGetToolInfo_WithOptionalFields(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:         "test-tool",
		Version:      "1.0.0",
		Description:  "A test tool",
		Entrypoint:   "bin/tool",
		Author:       "Test Author",
		License:      "MIT",
		Repository:   "https://github.com/test/tool",
		Homepage:     "https://example.com/tool",
		Keywords:     []string{"test", "example"},
		Dependencies: []string{"dep1", "dep2"},
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Author:      Test Author")
	assert.Contains(t, output, "License:     MIT")
	assert.Contains(t, output, "Repository:  https://github.com/test/tool")
	assert.Contains(t, output, "Homepage:    https://example.com/tool")
	assert.Contains(t, output, "Keywords:")
	assert.Contains(t, output, "Dependencies:")
	assert.Contains(t, output, "  - dep1")
	assert.Contains(t, output, "  - dep2")
}

func TestGetToolInfo_WithRuntime(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
			Env: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
			},
			Args: []string{"--arg1", "--arg2"},
		},
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Runtime Mode: capsule")
	assert.Contains(t, output, "Environment Variables:")
	assert.Contains(t, output, "  KEY1=value1")
	assert.Contains(t, output, "  KEY2=value2")
	assert.Contains(t, output, "Arguments:")
}

func TestGetToolInfo_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
		Author:      "Test Author",
		Keywords:    []string{"test"},
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		JSON:   true,
		Writer: &buf,
	})
	require.NoError(t, err)

	var info core.ToolManifest
	err = json.Unmarshal(buf.Bytes(), &info)
	require.NoError(t, err)

	assert.Equal(t, "test-tool", info.Name)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, "A test tool", info.Description)
	assert.Equal(t, "bin/tool", info.Entrypoint)
	assert.Equal(t, "Test Author", info.Author)
	assert.Equal(t, []string{"test"}, info.Keywords)
	assert.Equal(t, toolDir, info.Path)
}

func TestGetToolInfo_JSON_WithRuntime(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
		Runtime: &core.RuntimeConfig{
			Mode:             core.RuntimeModeCapsule,
			StartupTimeoutMs: 5000,
			Env: map[string]string{
				"KEY1": "value1",
			},
			Args: []string{"--arg1"},
		},
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		JSON:   true,
		Writer: &buf,
	})
	require.NoError(t, err)

	var info core.ToolManifest
	err = json.Unmarshal(buf.Bytes(), &info)
	require.NoError(t, err)

	assert.NotNil(t, info.Runtime)
	assert.Equal(t, core.RuntimeModeCapsule, info.Runtime.Mode)
	assert.Equal(t, 5000, info.Runtime.StartupTimeoutMs)
	assert.Equal(t, map[string]string{"KEY1": "value1"}, info.Runtime.Env)
	assert.Equal(t, []string{"--arg1"}, info.Runtime.Args)
}

func TestGetToolInfo_MultipleVersions(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool with version 1.0.0
	tool1Dir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1Dir, 0755))

	tool1Manifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool v1",
		Entrypoint:  "bin/tool",
	}
	manifestData1, err := yaml.Marshal(tool1Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1Dir, installer.ToolManifestFileName), manifestData1, 0644))

	// Create same tool with version 2.0.0 (should be selected as latest)
	tool2Dir := filepath.Join(tmpDir, "test-tool", "2.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool2Dir, 0755))

	tool2Manifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "2.0.0",
		Description: "Test tool v2",
		Entrypoint:  "bin/tool",
	}
	manifestData2, err := yaml.Marshal(tool2Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool2Dir, installer.ToolManifestFileName), manifestData2, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	// Should show version 2.0.0 (latest)
	assert.Contains(t, output, "Version:     2.0.0")
	assert.Contains(t, output, "Description: Test tool v2")
}

func TestGetToolInfo_NoValidVersions(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool directory but without a valid manifest
	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))
	// Don't create tool.yaml

	var buf bytes.Buffer
	err := GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find tool version")
}

func TestGetToolInfo_Error_GetInstallDir(t *testing.T) {
	// Mock GetInstalledToolsDir to return an error
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return "", assert.AnError
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get install directory")
}

func TestGetToolInfo_Error_LoadManifest(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool directory with invalid manifest
	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))
	// Create invalid YAML
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), []byte("invalid: yaml: content"), 0644))

	var buf bytes.Buffer
	err := GetToolInfo("test-tool", InfoOptions{
		Writer: &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load tool manifest")
}

func TestFindLatestToolVersion(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple versions
	tool1Dir := filepath.Join(tmpDir, "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1Dir, 0755))
	manifest1 := &core.ToolManifest{Name: "tool", Version: "1.0.0", Description: "Tool", Entrypoint: "bin/tool"}
	data1, yamlErr := yaml.Marshal(manifest1)
	require.NoError(t, yamlErr)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1Dir, installer.ToolManifestFileName), data1, 0644))

	tool2Dir := filepath.Join(tmpDir, "2.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool2Dir, 0755))
	manifest2 := &core.ToolManifest{Name: "tool", Version: "2.0.0", Description: "Tool", Entrypoint: "bin/tool"}
	data2, yamlErr := yaml.Marshal(manifest2)
	require.NoError(t, yamlErr)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool2Dir, installer.ToolManifestFileName), data2, 0644))

	toolDir, version, err := findLatestToolVersion(tmpDir)
	require.NoError(t, err)
	// Should return the latest version (2.0.0)
	assert.Equal(t, "2.0.0", version)
	assert.Equal(t, tool2Dir, toolDir)
}

func TestFindLatestToolVersion_NoValidVersions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory without manifest
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "1.0.0"), 0755))

	_, _, err := findLatestToolVersion(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid tool versions found")
}

func TestGetToolInfo_JSON_WithMCP(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
		MCP: &core.MCPConfig{
			InputSchema:  map[string]any{"type": "object"},
			OutputSchema: map[string]any{"type": "object"},
		},
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		JSON:   true,
		Writer: &buf,
	})
	require.NoError(t, err)

	var info core.ToolManifest
	err = json.Unmarshal(buf.Bytes(), &info)
	require.NoError(t, err)

	assert.NotNil(t, info.MCP)
	assert.Contains(t, buf.String(), "MCP")
}

func TestGetToolInfo_JSON_WithHotLoad(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
		Runtime: &core.RuntimeConfig{
			Mode: core.RuntimeModeCapsule,
			HotLoad: &core.HotLoadConfig{
				Mode:       core.HotLoadModeRestart,
				Watch:      []string{"src/**/*.go"},
				DebounceMs: 500,
			},
		},
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = GetToolInfo("test-tool", InfoOptions{
		JSON:   true,
		Writer: &buf,
	})
	require.NoError(t, err)

	var info core.ToolManifest
	err = json.Unmarshal(buf.Bytes(), &info)
	require.NoError(t, err)

	assert.NotNil(t, info.Runtime)
	assert.NotNil(t, info.Runtime.HotLoad)
	// HotLoad should be converted to JSON-compatible format
	// Verify it's present (exact type depends on JSON unmarshaling)
	hotLoadStr := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", info.Runtime.HotLoad)))
	assert.Contains(t, hotLoadStr, "restart")
}
