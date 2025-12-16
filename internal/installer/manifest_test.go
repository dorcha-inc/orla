package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLoadManifest(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "tool.yaml")

	manifest := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
	}

	data, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, data, 0600))

	// Create entrypoint file
	entrypointPath := filepath.Join(tmpDir, "bin", "tool")
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0700))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho test"), 0755))

	// Load manifest
	loaded, err := LoadManifest(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "test-tool", loaded.Name)
	assert.Equal(t, "1.0.0", loaded.Version)
	assert.Equal(t, "bin/tool", loaded.Entrypoint)
}

func TestValidateManifest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create entrypoint
	entrypointPath := filepath.Join(tmpDir, "bin", "tool")
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0700))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho test"), 0755))

	manifest := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
	}

	// Valid manifest
	err := ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err)

	// Missing name
	manifest.Name = ""
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Missing version
	manifest.Name = "test-tool"
	manifest.Version = ""
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Missing description
	manifest.Version = "1.0.0"
	manifest.Description = ""
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Missing entrypoint
	manifest.Description = "Test tool"
	manifest.Entrypoint = ""
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Invalid entrypoint path (outside tool directory)
	// os.Root will reject path traversal attempts, so we just verify an error occurs
	manifest.Entrypoint = "../../../etc/passwd"
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	// Verify it's not a "file not found" error - path traversal should be caught before that
	assert.NotContains(t, err.Error(), "does not exist")

	// Entrypoint file doesn't exist
	manifest.Entrypoint = "nonexistent/file"
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to validate entrypoint")
}

func TestValidateManifest_RuntimeMode(t *testing.T) {
	tmpDir := t.TempDir()

	entrypointPath := filepath.Join(tmpDir, "bin", "tool")
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0700))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho test"), 0755))

	// Valid manifest with simple runtime mode
	manifest := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
		Runtime: &RuntimeConfig{
			Mode: RuntimeModeSimple,
		},
	}
	err := ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, RuntimeModeSimple, manifest.Runtime.Mode)

	// Valid manifest with capsule runtime mode (should warn but succeed)
	manifest.Runtime.Mode = RuntimeModeCapsule
	err = ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, RuntimeModeCapsule, manifest.Runtime.Mode)

	// Invalid runtime mode
	manifest.Runtime.Mode = RuntimeMode("invalid")
	err = ValidateManifest(manifest, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid runtime.mode")

	// Nil runtime (should default to simple)
	manifest.Runtime = nil
	err = ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err)
	assert.NotNil(t, manifest.Runtime)
	assert.Equal(t, RuntimeModeSimple, manifest.Runtime.Mode)

	// Empty runtime mode (should default to simple)
	manifest.Runtime = &RuntimeConfig{Mode: ""}
	err = ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, RuntimeModeSimple, manifest.Runtime.Mode)
}

func TestValidateManifest_Executable(t *testing.T) {
	tmpDir := t.TempDir()

	entrypointPath := filepath.Join(tmpDir, "bin", "tool")
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0700))

	// Non-executable file (should still pass validation, just log debug)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho test"), 0600))
	manifest := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
	}
	err := ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err) // Non-executable files are allowed

	// Executable file
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho test"), 0755))
	err = ValidateManifest(manifest, tmpDir)
	assert.NoError(t, err)
}

func TestLoadManifest_ErrorCases(t *testing.T) {
	tmpDir := t.TempDir()

	// Test: tool.yaml doesn't exist
	_, err := LoadManifest(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read tool.yaml")

	// Test: invalid YAML
	manifestPath := filepath.Join(tmpDir, "tool.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte("invalid: yaml: content: [unclosed"), 0600))
	_, err = LoadManifest(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse tool.yaml")

	// Test: valid YAML but missing required fields
	require.NoError(t, os.WriteFile(manifestPath, []byte("name: test-tool\n# missing version"), 0600))
	_, err = LoadManifest(tmpDir)
	assert.NoError(t, err) // LoadManifest doesn't validate, just loads
}

func TestLoadManifest_OptionalFields(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "tool.yaml")

	// Test manifest with all optional fields
	manifest := &ToolManifest{
		Name:         "test-tool",
		Version:      "1.0.0",
		Description:  "Test tool",
		Entrypoint:   "bin/tool",
		Author:       "Test Author",
		License:      "MIT",
		Repository:   "https://github.com/test/tool",
		Homepage:     "https://example.com",
		Keywords:     []string{"test", "tool"},
		Dependencies: []string{"dep1", "dep2"},
		Runtime: &RuntimeConfig{
			Mode: RuntimeModeSimple,
		},
	}

	data, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, data, 0600))

	loaded, err := LoadManifest(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, "Test Author", loaded.Author)
	assert.Equal(t, "MIT", loaded.License)
	assert.Equal(t, "https://github.com/test/tool", loaded.Repository)
	assert.Equal(t, "https://example.com", loaded.Homepage)
	assert.Equal(t, []string{"test", "tool"}, loaded.Keywords)
	assert.Equal(t, []string{"dep1", "dep2"}, loaded.Dependencies)
	assert.NotNil(t, loaded.Runtime)
	assert.Equal(t, RuntimeModeSimple, loaded.Runtime.Mode)
}
