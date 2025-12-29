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

func TestScanInstalledTools(t *testing.T) {
	tmpDir := t.TempDir()

	// Create tool structure: TOOL-NAME/VERSION/tool.yaml
	toolDir := filepath.Join(tmpDir, "fs", "0.1.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	// Create tool.yaml
	manifest := &core.ToolManifest{
		Name:        "fs",
		Version:     "0.1.0",
		Description: "Filesystem tool",
		Entrypoint:  "bin/fs",
	}

	data, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), data, 0644))

	// Create entrypoint
	entrypointPath := filepath.Join(toolDir, "bin", "fs")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho fs"), 0755))

	// Scan installed tools
	tools, err := ScanInstalledTools(tmpDir)
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Contains(t, tools, "fs")
	assert.Equal(t, "Filesystem tool", tools["fs"].Description)
}

func TestScanInstalledTools_MultipleVersions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two versions of the same tool
	v1Dir := filepath.Join(tmpDir, "fs", "0.1.0")
	v2Dir := filepath.Join(tmpDir, "fs", "0.2.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(v1Dir, 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(v2Dir, 0755))

	// Create manifests
	manifest1 := &core.ToolManifest{
		Name:        "fs",
		Version:     "0.1.0",
		Description: "Filesystem tool v1",
		Entrypoint:  "bin/fs",
	}
	manifest2 := &core.ToolManifest{
		Name:        "fs",
		Version:     "0.2.0",
		Description: "Filesystem tool v2",
		Entrypoint:  "bin/fs",
	}

	data1, err := yaml.Marshal(manifest1)
	require.NoError(t, err)
	data2, err := yaml.Marshal(manifest2)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(v1Dir, "tool.yaml"), data1, 0644))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(v2Dir, "tool.yaml"), data2, 0644))

	// Create entrypoints
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(v1Dir, "bin"), 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(v2Dir, "bin"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(v1Dir, "bin", "fs"), []byte("#!/bin/sh"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(v2Dir, "bin", "fs"), []byte("#!/bin/sh"), 0755))

	// Scan installed tools - should prefer latest version
	tools, err := ScanInstalledTools(tmpDir)
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	// Should use the newer version (0.2.0)
	assert.Equal(t, "Filesystem tool v2", tools["fs"].Description)
}

func TestScanInstalledTools_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tools, err := ScanInstalledTools(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestScanInstalledTools_NonexistentDirectory(t *testing.T) {
	tools, err := ScanInstalledTools("/nonexistent/directory")
	require.NoError(t, err) // Should return empty map, not error
	assert.Empty(t, tools)
}

func TestScanInstalledTools_NotADirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "notadir")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0644))

	tools, err := ScanInstalledTools(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	assert.Nil(t, tools)
}

func TestScanInstalledTools_InvalidManifest(t *testing.T) {
	tmpDir := t.TempDir()

	// Create tool directory with invalid manifest
	toolDir := filepath.Join(tmpDir, "fs", "0.1.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	// Create invalid YAML
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), []byte("invalid: yaml: content"), 0644))

	// Scan should skip invalid manifests and return empty map
	tools, err := ScanInstalledTools(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, tools) // Invalid manifest should be skipped
}

func TestScanInstalledTools_ManifestValidationFailed(t *testing.T) {
	tmpDir := t.TempDir()

	// Create tool directory with manifest that fails validation
	toolDir := filepath.Join(tmpDir, "fs", "0.1.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	// Create manifest with missing required field (description)
	manifest := &core.ToolManifest{
		Name:       "fs",
		Version:    "0.1.0",
		Entrypoint: "bin/fs",
		// Missing Description - should fail validation
	}

	data, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), data, 0644))

	// Scan should skip invalid manifests and return empty map
	tools, err := ScanInstalledTools(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, tools) // Invalid manifest should be skipped
}

func TestScanInstalledTools_EntrypointNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	// Create tool directory with manifest but missing entrypoint
	toolDir := filepath.Join(tmpDir, "fs", "0.1.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	// Create valid manifest
	manifest := &core.ToolManifest{
		Name:        "fs",
		Version:     "0.1.0",
		Description: "Filesystem tool",
		Entrypoint:  "bin/fs", // Entrypoint doesn't exist
	}

	data, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), data, 0644))

	// Scan should skip tools with missing entrypoints
	tools, err := ScanInstalledTools(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, tools) // Missing entrypoint should be skipped
}

func TestGetVersionFromPath(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (toolPath string, installDirPath string)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid path",
			setup: func() (string, string) {
				tmpDir := t.TempDir()
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolPath := filepath.Join(installDir, "fs", "0.1.0", "bin", "fs")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
				// #nosec G306 -- test file permissions are acceptable for temporary test files
				require.NoError(t, os.WriteFile(toolPath, []byte("test"), 0644))
				return toolPath, installDir
			},
			want:    "0.1.0",
			wantErr: false,
		},
		{
			name: "path with less than 2 parts",
			setup: func() (string, string) {
				tmpDir := t.TempDir()
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolPath := filepath.Join(installDir, "fs")
				// #nosec G306 -- test file permissions are acceptable for temporary test files
				require.NoError(t, os.WriteFile(toolPath, []byte("test"), 0644))
				return toolPath, installDir
			},
			want:        "",
			wantErr:     true,
			errContains: "not within install directory",
		},
		{
			name: "nested path",
			setup: func() (string, string) {
				tmpDir := t.TempDir()
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolPath := filepath.Join(installDir, "fs", "0.2.0", "bin", "subdir", "tool")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
				// #nosec G306 -- test file permissions are acceptable for temporary test files
				require.NoError(t, os.WriteFile(toolPath, []byte("test"), 0644))
				return toolPath, installDir
			},
			want:    "0.2.0",
			wantErr: false,
		},
		{
			name: "path outside install directory",
			setup: func() (string, string) {
				tmpDir := t.TempDir()
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				otherDir := filepath.Join(tmpDir, "other")
				toolPath := filepath.Join(otherDir, "fs", "0.1.0", "bin", "fs")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
				// #nosec G306 -- test file permissions are acceptable for temporary test files
				require.NoError(t, os.WriteFile(toolPath, []byte("test"), 0644))
				return toolPath, installDir
			},
			want:        "",
			wantErr:     true,
			errContains: "not within install directory",
		},
		{
			name: "path with parent directory traversal",
			setup: func() (string, string) {
				tmpDir := t.TempDir()
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				// Create a path that would escape via ".."
				otherDir := filepath.Join(tmpDir, "other")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(otherDir, 0755))
				// Use a path that resolves outside installDir
				toolPath := filepath.Join(installDir, "..", "other", "fs", "0.1.0", "bin", "fs")
				// Resolve to absolute to test the actual resolved path
				absToolPath, err := filepath.Abs(toolPath)
				require.NoError(t, err)
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(filepath.Dir(absToolPath), 0755))
				// #nosec G306 -- test file permissions are acceptable for temporary test files
				require.NoError(t, os.WriteFile(absToolPath, []byte("test"), 0644))
				return absToolPath, installDir
			},
			want:        "",
			wantErr:     true,
			errContains: "not within install directory",
		},
		{
			name: "invalid semver version",
			setup: func() (string, string) {
				tmpDir := t.TempDir()
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolPath := filepath.Join(installDir, "fs", "invalid-version", "bin", "fs")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
				// #nosec G306 -- test file permissions are acceptable for temporary test files
				require.NoError(t, os.WriteFile(toolPath, []byte("test"), 0644))
				return toolPath, installDir
			},
			want:        "",
			wantErr:     true,
			errContains: "invalid version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolPath, installDirPath := tt.setup()
			got, err := getVersionFromPath(toolPath, installDirPath)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestScanToolsFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create executable files
	tool1 := filepath.Join(toolsDir, "tool1.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(tool1, []byte("#!/bin/sh\necho tool1"), 0755))

	tool2 := filepath.Join(toolsDir, "tool2.py")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(tool2, []byte("#!/usr/bin/python3\nprint('tool2')"), 0755))

	// Scan tools
	tools, err := ScanToolsFromDirectory(toolsDir)
	require.NoError(t, err)
	assert.Len(t, tools, 2)
	assert.Contains(t, tools, "tool1")
	assert.Contains(t, tools, "tool2")
}

func TestScanToolsFromDirectory_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	tools, err := ScanToolsFromDirectory(toolsDir)
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestScanToolsFromDirectory_NonexistentDirectory(t *testing.T) {
	tools, err := ScanToolsFromDirectory("/nonexistent/directory")
	require.NoError(t, err) // Should return empty map, not error
	assert.Empty(t, tools)
}

func TestScanToolsFromDirectory_NotADirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "notadir")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0644))

	tools, err := ScanToolsFromDirectory(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	assert.Nil(t, tools)
}

func TestScanToolsFromDirectory_DuplicateToolName(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create two files with the same base name (different extensions)
	tool1 := filepath.Join(toolsDir, "mytool.sh")
	tool2 := filepath.Join(toolsDir, "mytool.py")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(tool1, []byte("#!/bin/sh"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(tool2, []byte("#!/usr/bin/python3"), 0755))

	// Should return error for duplicate tool name
	tools, err := ScanToolsFromDirectory(toolsDir)
	assert.Error(t, err)
	assert.Nil(t, tools)
	var dupErr *DuplicateToolNameError
	assert.ErrorAs(t, err, &dupErr)
	assert.Equal(t, "mytool", dupErr.Name)
}

func TestScanToolsFromDirectory_NonExecutableFile(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create non-executable file
	nonExec := filepath.Join(toolsDir, "readme.txt")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(nonExec, []byte("readme content"), 0644))

	// Create executable file
	execTool := filepath.Join(toolsDir, "tool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(execTool, []byte("#!/bin/sh"), 0755))

	// Should only find executable file
	tools, err := ScanToolsFromDirectory(toolsDir)
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Contains(t, tools, "tool")
}

func TestScanToolsFromDirectory_Subdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create subdirectory with executable
	subDir := filepath.Join(toolsDir, "subdir")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(subDir, 0755))
	subTool := filepath.Join(subDir, "subtool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(subTool, []byte("#!/bin/sh"), 0755))

	// Should find tool in subdirectory
	tools, err := ScanToolsFromDirectory(toolsDir)
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Contains(t, tools, "subtool")
}

// TestGetVersionFromPath_RelError tests error path when filepath.Rel fails
func TestGetVersionFromPath_RelError(t *testing.T) {
	// filepath.Rel can fail if paths are on different volumes (Windows) or other edge cases
	// We test with a path outside installDir to trigger the error path
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	otherDir := filepath.Join(tmpDir, "other")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(otherDir, 0755))

	toolPath := filepath.Join(otherDir, "fs", "0.1.0", "bin", "fs")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(toolPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(toolPath, []byte("test"), 0644))

	// This should fail because toolPath is outside installDir
	_, err := getVersionFromPath(toolPath, installDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not within install directory")
}

// TestGetVersionFromPath_StatError tests error path when root.Stat fails
func TestGetVersionFromPath_StatError(t *testing.T) {
	tmpDir := t.TempDir()
	installDir := filepath.Join(tmpDir, "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(installDir, 0755))

	// Use a path that doesn't exist - root.Stat will fail
	nonExistentPath := filepath.Join(installDir, "nonexistent", "0.1.0", "bin", "fs")

	_, err := getVersionFromPath(nonExistentPath, installDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not within install directory")
}

func TestScanInstalledTools_InvalidShebang(t *testing.T) {
	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, ".orla", "tools")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolsDir, 0755))

	// Create a file with an invalid shebang
	tool := filepath.Join(toolsDir, "mytool.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(tool, []byte("#invalid-shebang"), 0755))

	// Should return empty map for invalid shebang
	tools, err := ScanInstalledTools(toolsDir)
	require.NoError(t, err)
	assert.Empty(t, tools)
}
