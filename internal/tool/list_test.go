package tool

import (
	"bytes"
	"encoding/json"
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

func TestListTools_EmptyList_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := ListTools(ListOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No tools installed.")
	assert.Contains(t, output, "orla tool install TOOL-NAME")
}

func TestListTools_EmptyList_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := ListTools(ListOptions{
		JSON:   true,
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Equal(t, "[]", output)
}

func TestListTools_SingleTool_Simple(t *testing.T) {
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
	err = ListTools(ListOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-tool (1.0.0)")
}

func TestListTools_MultipleTools_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create first tool
	tool1Dir := filepath.Join(tmpDir, "tool1", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1Dir, 0755))

	tool1Manifest := &core.ToolManifest{
		Name:        "tool1",
		Version:     "1.0.0",
		Description: "First tool",
		Entrypoint:  "bin/tool1",
	}
	manifestData1, err := yaml.Marshal(tool1Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1Dir, installer.ToolManifestFileName), manifestData1, 0644))

	// Create second tool
	tool2Dir := filepath.Join(tmpDir, "tool2", "2.5.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool2Dir, 0755))

	tool2Manifest := &core.ToolManifest{
		Name:        "tool2",
		Version:     "2.5.0",
		Description: "Second tool",
		Entrypoint:  "bin/tool2",
	}
	manifestData2, err := yaml.Marshal(tool2Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool2Dir, installer.ToolManifestFileName), manifestData2, 0644))

	var buf bytes.Buffer
	err = ListTools(ListOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "tool1 (1.0.0)")
	assert.Contains(t, output, "tool2 (2.5.0)")
}

func TestListTools_MultipleVersions_Simple(t *testing.T) {
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

	// Create same tool with version 2.0.0
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
	err = ListTools(ListOptions{
		Writer: &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-tool (1.0.0)")
	assert.Contains(t, output, "test-tool (2.0.0)")
}

func TestListTools_Verbose(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool
	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool with a description",
		Entrypoint:  "bin/tool",
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = ListTools(ListOptions{
		Verbose: true,
		Writer:  &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.GreaterOrEqual(t, len(lines), 3) // Header, separator, and at least one tool
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "DESCRIPTION")
	assert.Contains(t, output, "test-tool")
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "A test tool with a description")
}

func TestListTools_Verbose_LongDescription(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool with long description
	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	longDescription := strings.Repeat("This is a very long description. ", 10) // Should be > 60 chars
	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: longDescription,
		Entrypoint:  "bin/tool",
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, installer.ToolManifestFileName), manifestData, 0644))

	var buf bytes.Buffer
	err = ListTools(ListOptions{
		Verbose: true,
		Writer:  &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	// Description should be truncated to 60 chars with "..."
	assert.Contains(t, output, "...")
	// Find the line with the tool and verify description is truncated
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "test-tool") {
			// Description should be at most 60 chars (57 + "...")
			parts := strings.Split(line, "\t")
			if len(parts) >= 3 {
				description := parts[2]
				assert.LessOrEqual(t, len(description), 60)
				assert.True(t, strings.HasSuffix(description, "...") || len(description) <= 60)
			}
		}
	}
}

func TestListTools_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create first tool
	tool1Dir := filepath.Join(tmpDir, "tool1", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1Dir, 0755))

	tool1Manifest := &core.ToolManifest{
		Name:        "tool1",
		Version:     "1.0.0",
		Description: "First tool",
		Entrypoint:  "bin/tool1",
	}
	manifestData1, err := yaml.Marshal(tool1Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1Dir, installer.ToolManifestFileName), manifestData1, 0644))

	// Create second tool
	tool2Dir := filepath.Join(tmpDir, "tool2", "2.5.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool2Dir, 0755))

	tool2Manifest := &core.ToolManifest{
		Name:        "tool2",
		Version:     "2.5.0",
		Description: "Second tool",
		Entrypoint:  "bin/tool2",
	}
	manifestData2, err := yaml.Marshal(tool2Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool2Dir, installer.ToolManifestFileName), manifestData2, 0644))

	var buf bytes.Buffer
	err = ListTools(ListOptions{
		JSON:   true,
		Writer: &buf,
	})
	require.NoError(t, err)

	var tools []installer.InstalledToolInfo
	err = json.Unmarshal(buf.Bytes(), &tools)
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	// Verify tool data
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
		switch tool.Name {
		case "tool1":
			assert.Equal(t, "1.0.0", tool.Version)
			assert.Equal(t, "First tool", tool.Description)
		case "tool2":
			assert.Equal(t, "2.5.0", tool.Version)
			assert.Equal(t, "Second tool", tool.Description)
		}
	}
	assert.True(t, toolNames["tool1"])
	assert.True(t, toolNames["tool2"])
}

func TestListTools_DefaultWriter(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool
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

	// Test with nil Writer (should default to os.Stdout)
	err = ListTools(ListOptions{
		Writer: nil,
	})
	// Should not error (we can't easily capture stdout in tests, so we just verify no error)
	require.NoError(t, err)
}

func TestListTools_ErrorFromListInstalledTools(t *testing.T) {
	// Mock GetInstalledToolsDir to return an error
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return "", assert.AnError
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := ListTools(ListOptions{
		Writer: &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list installed tools")
}
