package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create executable file
	execPath := filepath.Join(tmpDir, "executable.sh")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(execPath, []byte("#!/bin/sh\necho test"), 0755))
	// #nosec G304 -- path is constructed from test temp directory, safe
	info, err := os.Stat(execPath)
	require.NoError(t, err)
	assert.True(t, IsExecutable(info))

	// Create non-executable file
	nonExecPath := filepath.Join(tmpDir, "non-executable.txt")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(nonExecPath, []byte("test content"), 0644))
	// #nosec G304 -- path is constructed from test temp directory, safe
	info, err = os.Stat(nonExecPath)
	require.NoError(t, err)
	assert.False(t, IsExecutable(info))

	// Test executable directory
	dirPath := filepath.Join(tmpDir, "subdir")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(dirPath, 0755))
	// #nosec G304 -- path is constructed from test temp directory, safe
	info, err = os.Stat(dirPath)
	require.NoError(t, err)
	assert.True(t, IsExecutable(info))

	// Test non-executable directory
	dirPath = filepath.Join(tmpDir, "non-executable-dir")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(dirPath, 0644))
	// #nosec G304 -- path is constructed from test temp directory, safe
	info, err = os.Stat(dirPath)
	require.NoError(t, err)
	assert.False(t, IsExecutable(info))

}

func TestCopyDirectory(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source directory structure with specific permissions
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file2.txt"), []byte("content2"), 0755))
	// Create subdirectory with specific permissions to test preservation
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "subdir"), 0700))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "subdir", "file3.txt"), []byte("content3"), 0644))
	// Create .git directory that should be skipped
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, ".git"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, ".git", "config"), []byte("git config"), 0644))

	// Ensure destination directory exists (os.Root requires it)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(dstDir, 0755))

	// Copy directory
	err := CopyDirectory(srcDir, dstDir, []string{".git"})
	require.NoError(t, err)

	// Verify files were copied
	// #nosec G304 -- paths are constructed from test temp directories, safe
	data, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("content1"), data)

	// #nosec G304 -- paths are constructed from test temp directories, safe
	data, err = os.ReadFile(filepath.Join(dstDir, "file2.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("content2"), data)

	// #nosec G304 -- paths are constructed from test temp directories, safe
	data, err = os.ReadFile(filepath.Join(dstDir, "subdir", "file3.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("content3"), data)

	// Verify directory permissions were preserved
	// #nosec G304 -- paths are constructed from test temp directories, safe
	srcSubdirInfo, err := os.Stat(filepath.Join(srcDir, "subdir"))
	require.NoError(t, err)
	// #nosec G304 -- paths are constructed from test temp directories, safe
	dstSubdirInfo, err := os.Stat(filepath.Join(dstDir, "subdir"))
	require.NoError(t, err)
	assert.Equal(t, srcSubdirInfo.Mode().Perm(), dstSubdirInfo.Mode().Perm(), "directory permissions should be preserved")

	// Verify .git was skipped
	// #nosec G304 -- paths are constructed from test temp directories, safe
	_, err = os.Stat(filepath.Join(dstDir, ".git"))
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestCopyDirectory_EmptySkipDirs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0644))

	// Ensure destination directory exists
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(dstDir, 0755))

	// Copy without skipping anything
	err := CopyDirectory(srcDir, dstDir, nil)
	require.NoError(t, err)

	// #nosec G304 -- paths are constructed from test temp directories, safe
	data, err := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("content"), data)
}

func TestCopyDirectory_ErrorCases(t *testing.T) {
	// Test: source directory doesn't exist
	err := CopyDirectory("/nonexistent/dir", t.TempDir(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open source directory")

	// Test: destination directory creation fails (invalid path)
	err = CopyDirectory(t.TempDir(), "/root/invalid/path", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open destination directory")
}
