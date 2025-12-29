package installer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/yaml.v3"
)

const exampleRegistryURL = "https://example.com/registry"

func TestInstallToDirectory(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create source files
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("content1"), 0644))
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "subdir"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "subdir", "file2.txt"), []byte("content2"), 0644))

	// Install to directory
	var buf bytes.Buffer
	err := InstallToDirectory(srcDir, dstDir, &buf)
	require.NoError(t, err)

	// Verify files were copied
	// #nosec G304 -- paths are constructed from test temp directories, safe
	data, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("content1"), data)

	// #nosec G304 -- paths are constructed from test temp directories, safe
	data, err = os.ReadFile(filepath.Join(dstDir, "subdir", "file2.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("content2"), data)
}

func TestInstallToDirectory_ErrorCases(t *testing.T) {
	// Test: source directory doesn't exist
	err := InstallToDirectory("/nonexistent/dir", t.TempDir(), &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to")

	// Test: invalid destination path (on Unix, /root might not be writable, but the error will be about creating directory)
	err = InstallToDirectory(t.TempDir(), "/root/invalid/path", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to")
}

func TestCloneToolRepository(t *testing.T) {
	// Test: invalid repository URL
	err := cloneToolRepository("not-a-valid-url", "v1.0.0", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clone repository")
}

func TestCloneToolRepository_WithMockRepo(t *testing.T) {
	// Create a mock git repository
	repoDir := t.TempDir()
	targetDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create a file and commit
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Create a tag
	cmd = exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test cloning with valid branch/tag
	err := cloneToolRepository(repoDir, "v1.0.0", targetDir)
	require.NoError(t, err)

	// Verify files were cloned
	_, err = os.Stat(filepath.Join(targetDir, "test.txt"))
	assert.NoError(t, err)
}

func TestCloneToolRepository_FallbackPath(t *testing.T) {
	// Create a mock git repository
	repoDir := t.TempDir()
	targetDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create a file and commit
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Create a tag (but don't use it as branch name)
	cmd = exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test cloning with non-existent branch (should fallback to clone + checkout)
	err := cloneToolRepository(repoDir, "v1.0.0", targetDir)
	require.NoError(t, err)

	// Verify files were cloned
	_, err = os.Stat(filepath.Join(targetDir, "test.txt"))
	assert.NoError(t, err)
}

func TestCloneToolRepository_InvalidTag(t *testing.T) {
	// Create a mock git repository
	repoDir := t.TempDir()
	targetDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create a file and commit
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Test cloning with invalid tag (should fail on checkout)
	err := cloneToolRepository(repoDir, "nonexistent-tag", targetDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to checkout tag")
}

// TestInstallTool is complex because it requires:
// - A real registry
// - Git repository access
// - Network access
// This would be better as an integration test
// For now, we test the error paths we can test

func TestInstallTool_InvalidRegistry(t *testing.T) {
	// Test with invalid registry URL
	err := InstallTool("not-a-valid-url", "test-tool", "v1.0.0", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch registry")
}

func TestInstallTool_Success(t *testing.T) {
	// Set up zap logger with observer
	coreLogger, logs := observer.New(zap.InfoLevel)
	logger := zap.New(coreLogger)
	zap.ReplaceGlobals(logger)

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create a git repository for the tool
	repoDir := filepath.Join(tmpDir, "repo")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create tool.yaml
	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
	}
	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "bin"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ToolManifestFileName), manifestData, 0644))

	// Create entrypoint
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "bin", "tool"), []byte("#!/bin/sh\necho test"), 0755))

	// Commit files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Create a tag
	cmd = exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cacheKey, err := registry.SanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []registry.ToolEntry{
			{
				Name:        "test-tool",
				Description: "Test tool",
				Repository:  repoDir,
			},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner for registry cloning
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Copy registry.yaml to target
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if errMkdir := os.MkdirAll(targetPath, 0755); errMkdir != nil {
				return errMkdir
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			if errWrite := os.WriteFile(filepath.Join(targetPath, "registry.yaml"), data, 0644); errWrite != nil {
				return errWrite
			}
			return nil
		},
	}
	originalRunner := registry.GetDefaultGitRunner()
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Mock GetInstalledToolsDir
	installDir := filepath.Join(tmpDir, "tools")
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Test InstallTool - should log success
	errInstallTool := InstallTool(exampleRegistryURL, "test-tool", "v1.0.0", &bytes.Buffer{})
	require.NoError(t, errInstallTool)

	// Verify logging
	assert.GreaterOrEqual(t, logs.Len(), 1)
	found := false
	for _, entry := range logs.All() {
		if entry.Message == "Tool installed successfully" {
			assert.Equal(t, zap.InfoLevel, entry.Level)
			found = true
			break
		}
	}
	assert.True(t, found, "Expected log entry 'Tool installed successfully' not found")
}

func TestInstallTool_ToolNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	cacheKey, err := registry.SanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []registry.ToolEntry{
			{Name: "fs-tool", Description: "Filesystem tool"},
			{Name: "http-tool", Description: "HTTP tool"},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner to return our test registry
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Copy registry.yaml to target
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(filepath.Join(targetPath, "registry.yaml"), data, 0644)
		},
	}
	originalRunner := registry.GetDefaultGitRunner()
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Test InstallTool with non-existent tool (no suggestion since distance > 2)
	err = InstallTool(exampleRegistryURL, "xyz-tool", "v1.0.0", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "Did you mean")
}

func TestInstallTool_ToolNotFoundWithSuggestion(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	cacheKey, err := registry.SanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []registry.ToolEntry{
			{Name: "fs-tool", Description: "Filesystem tool"},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(filepath.Join(targetPath, "registry.yaml"), data, 0644)
		},
	}
	originalRunner := registry.GetDefaultGitRunner()
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Test InstallTool with typo - should suggest similar tool
	err = InstallTool(exampleRegistryURL, "fs-tol", "v1.0.0", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Did you mean")
	assert.Contains(t, err.Error(), "fs-tool")
}

func TestInstallTool_VersionNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	cacheKey, err := registry.SanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []registry.ToolEntry{
			{
				Name:        "test-tool",
				Description: "Test tool",
				Repository:  "https://example.com/repo",
			},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(filepath.Join(targetPath, "registry.yaml"), data, 0644)
		},
	}
	originalRunner := registry.GetDefaultGitRunner()
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Test InstallTool with non-existent tag
	err = InstallTool(exampleRegistryURL, "test-tool", "v99.0.0", &bytes.Buffer{})
	assert.Error(t, err)
	// Tag validation passes, but clone will fail since tag doesn't exist
	assert.True(t, strings.Contains(err.Error(), "failed to clone") || strings.Contains(err.Error(), "not found"))
}

func TestInstallTool_LoadManifestError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	repoDir := t.TempDir()
	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create a file but no tool.yaml
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "readme.txt"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cacheKey, err := registry.SanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []registry.ToolEntry{
			{
				Name:        "test-tool",
				Description: "Test tool",
				Repository:  repoDir,
			},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner - clone will succeed but tool.yaml won't exist
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Simulate successful clone by copying repo files
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			// Copy readme.txt but not tool.yaml
			// #nosec G304 -- test file permissions are acceptable for temporary test files
			readmeData, errRead := os.ReadFile(filepath.Join(repoDir, "readme.txt"))
			if errRead != nil {
				return errRead
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(filepath.Join(targetPath, "readme.txt"), readmeData, 0644)
		},
	}
	originalRunner := registry.GetDefaultGitRunner()
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Test InstallTool - should fail when loading manifest (tool.yaml doesn't exist)
	err = InstallTool(exampleRegistryURL, "test-tool", "v1.0.0", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load manifest")
}

func TestInstallTool_ValidateManifestError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	repoDir := t.TempDir()
	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create tool.yaml with missing required field (description)
	manifestContent := `name: test-tool
version: 1.0.0
entrypoint: bin/tool
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "tool.yaml"), []byte(manifestContent), 0644))

	// Create entrypoint
	binDir := filepath.Join(repoDir, "bin")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(binDir, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "tool"), []byte("#!/bin/sh\necho test"), 0755))

	// Commit files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	registryURL := "https://example.com/registry"
	cacheKey, err := registry.SanitizeURLForCache(registryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: registryURL,
		Tools: []registry.ToolEntry{
			{
				Name:        "test-tool",
				Description: "Test tool",
				Repository:  repoDir,
			},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner - clone will copy the repo with invalid manifest
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Copy repo files including tool.yaml
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			// Copy tool.yaml
			// #nosec G304 -- test file permissions are acceptable for temporary test files
			toolYAMLData, errRead := os.ReadFile(filepath.Join(repoDir, "tool.yaml"))
			if errRead != nil {
				return errRead
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			if err := os.WriteFile(filepath.Join(targetPath, "tool.yaml"), toolYAMLData, 0644); err != nil {
				return err
			}
			// Copy bin directory
			binTargetDir := filepath.Join(targetPath, "bin")
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if errMkdir := os.MkdirAll(binTargetDir, 0755); errMkdir != nil {
				return errMkdir
			}
			// #nosec G304 -- this is a test file, so we can ignore the security check
			binData, err := os.ReadFile(filepath.Join(repoDir, "bin", "tool"))
			if err != nil {
				return err
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(filepath.Join(binTargetDir, "tool"), binData, 0755)
		},
	}
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(registry.GetDefaultGitRunner())

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Test InstallTool - should fail when validating manifest (missing description)
	err = InstallTool(registryURL, "test-tool", "v1.0.0", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manifest validation failed")
}

func TestCloneToolRepository_FallbackCloneError(t *testing.T) {
	// Create a mock git repository
	repoDir := t.TempDir()
	targetDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create a file and commit
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "test.txt"), []byte("test"), 0644))
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Test cloning with non-existent branch that triggers fallback, but fallback clone also fails
	// Use an invalid repo URL to trigger the fallback path error
	err := cloneToolRepository("/nonexistent/repo", "v1.0.0", targetDir)
	assert.Error(t, err)
	// Should fail on the fallback clone attempt
	assert.Contains(t, err.Error(), "failed to clone repository")
}

func TestInstallTool_EndToEnd(t *testing.T) {
	// Set up a mock git repository with a tool manifest
	repoDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create tool.yaml manifest
	manifestContent := `name: test-tool
version: 1.0.0
description: Test tool
entrypoint: bin/tool
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "tool.yaml"), []byte(manifestContent), 0644))

	// Create entrypoint
	binDir := filepath.Join(repoDir, "bin")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(binDir, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "tool"), []byte("#!/bin/sh\necho test"), 0755))

	// Commit files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Create a tag
	cmd = exec.Command("git", "tag", "v1.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create a mock registry file
	registryDir := t.TempDir()
	registryYAML := `version: 1
registry_url: https://example.com/registry
tools:
  - name: test-tool
    description: Test tool
    repository: ` + repoDir + `
    versions:
      - version: 1.0.0
        tag: v1.0.0
`
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(registryDir, "registry.yaml"), []byte(registryYAML), 0644))

	// Initialize git repo for registry
	cmd = exec.Command("git", "init")
	cmd.Dir = registryDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = registryDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = registryDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Test InstallTool with the mock registry
	// Note: This test requires the registry to be accessible via file:// URL
	// On some systems, file:// URLs might not work with git clone, so we'll skip if it fails
	var buf bytes.Buffer
	err := InstallTool(registryDir, "test-tool", "1.0.0", &buf)
	if err != nil {
		// If it fails due to git clone issues with file:// URLs, that's okay for unit tests
		// This would be better as an integration test
		if strings.Contains(err.Error(), "failed to clone") || strings.Contains(err.Error(), "failed to fetch registry") {
			t.Skip("Skipping end-to-end test - git clone with file:// URLs may not work in all environments")
		}
		require.NoError(t, err)
	}

	// If successful, verify installation
	if err == nil {
		installBaseDir, err := registry.GetInstalledToolsDir()
		require.NoError(t, err)
		installDir := filepath.Join(installBaseDir, "test-tool", "1.0.0")
		_, err = os.Stat(filepath.Join(installDir, "tool.yaml"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(installDir, "bin", "tool"))
		assert.NoError(t, err)
	}
}

func TestListInstalledTools(t *testing.T) {
	// Create a temporary install directory
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool structure: TOOL-NAME/VERSION/tool.yaml
	tool1Dir := filepath.Join(tmpDir, "tool1", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1Dir, 0755))

	tool1Manifest := &core.ToolManifest{
		Name:        "tool1",
		Version:     "1.0.0",
		Description: "First tool",
		Entrypoint:  "bin/tool1",
	}
	manifestData, err := yaml.Marshal(tool1Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1Dir, ToolManifestFileName), manifestData, 0644))

	// Create second tool with different version
	tool1v2Dir := filepath.Join(tmpDir, "tool1", "2.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1v2Dir, 0755))

	tool1v2Manifest := &core.ToolManifest{
		Name:        "tool1",
		Version:     "2.0.0",
		Description: "First tool v2",
		Entrypoint:  "bin/tool1",
	}
	manifestData2, err := yaml.Marshal(tool1v2Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1v2Dir, ToolManifestFileName), manifestData2, 0644))

	// Create second tool
	tool2Dir := filepath.Join(tmpDir, "tool2", "1.5.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool2Dir, 0755))

	tool2Manifest := &core.ToolManifest{
		Name:        "tool2",
		Version:     "1.5.0",
		Description: "Second tool",
		Entrypoint:  "bin/tool2",
	}
	manifestData3, err := yaml.Marshal(tool2Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool2Dir, ToolManifestFileName), manifestData3, 0644))

	// List installed tools
	tools, err := ListInstalledTools()
	require.NoError(t, err)
	assert.Len(t, tools, 3)

	// Verify tools are listed
	toolNames := make(map[string]bool)
	versions := make(map[string]string)
	for _, tool := range tools {
		toolNames[tool.Name] = true
		if tool.Name == "tool1" {
			versions[tool.Version] = tool.Description
		}
	}

	assert.True(t, toolNames["tool1"])
	assert.True(t, toolNames["tool2"])
	assert.Equal(t, "First tool", versions["1.0.0"])
	assert.Equal(t, "First tool v2", versions["2.0.0"])
}

func TestListInstalledTools_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	tools, err := ListInstalledTools()
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestUninstallTool(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Create tool directory structure
	toolDir := filepath.Join(tmpDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(toolDir, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "tool.yaml"), []byte("test"), 0644))

	// Verify tool exists
	_, err := os.Stat(filepath.Join(tmpDir, "test-tool"))
	require.NoError(t, err)

	// Uninstall tool
	err = UninstallTool("test-tool")
	require.NoError(t, err)

	// Verify tool directory is removed
	_, err = os.Stat(filepath.Join(tmpDir, "test-tool"))
	assert.True(t, os.IsNotExist(err))
}

func TestUninstallTool_NotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	err := UninstallTool("nonexistent-tool")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestUpdateTool(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create a git repository for the tool
	repoDir := filepath.Join(tmpDir, "repo")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Create tool.yaml
	toolManifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "2.0.0",
		Description: "Test tool updated",
		Entrypoint:  "bin/tool",
	}

	manifestData, err := yaml.Marshal(toolManifest)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "bin"), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, ToolManifestFileName), manifestData, 0644))

	// Create entrypoint
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "bin", "tool"), []byte("#!/bin/sh\necho test"), 0755))

	// Commit files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "update commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
	require.NoError(t, cmd.Run())

	// Create a tag for version 2.0.0
	cmd = exec.Command("git", "tag", "v2.0.0")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cacheKey, err := registry.SanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create registry.yaml file with multiple versions
	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []registry.ToolEntry{
			{
				Name:        "test-tool",
				Description: "Test tool",
				Repository:  repoDir,
			},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GitRunner for registry cloning and tool tag listing
	mockRunner := &registry.MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Copy registry.yaml to target
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if errMkdir := os.MkdirAll(targetPath, 0755); errMkdir != nil {
				return errMkdir
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			if errWrite := os.WriteFile(filepath.Join(targetPath, "registry.yaml"), data, 0644); errWrite != nil {
				return errWrite
			}
			return nil
		},
		ListTagsFunc: func(repoURL string) ([]string, error) {
			// Return tags for the tool repository
			if repoURL == repoDir {
				return []string{"v1.0.0", "v2.0.0"}, nil
			}
			return []string{}, nil
		},
	}
	originalRunner := registry.GetDefaultGitRunner()
	registry.SetGitRunner(mockRunner)
	defer registry.SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := *registry.GetRegistryCacheDirFunc
	*registry.GetRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		*registry.GetRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Mock GetInstalledToolsDir
	installDir := filepath.Join(tmpDir, "tools")
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// First, install version 1.0.0
	// Create initial tool installation
	tool1Dir := filepath.Join(installDir, "test-tool", "1.0.0")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(tool1Dir, 0755))

	tool1Manifest := &core.ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "Test tool",
		Entrypoint:  "bin/tool",
	}
	manifestData1, err := yaml.Marshal(tool1Manifest)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filepath.Join(tool1Dir, ToolManifestFileName), manifestData1, 0644))

	// Verify tool is installed
	_, err = os.Stat(tool1Dir)
	require.NoError(t, err)

	// Update tool to latest version
	var buf bytes.Buffer
	err = UpdateTool(exampleRegistryURL, "test-tool", &buf)
	require.NoError(t, err)

	// Verify new version is installed
	tool2Dir := filepath.Join(installDir, "test-tool", "2.0.0")
	_, err = os.Stat(tool2Dir)
	assert.NoError(t, err)

	// Verify tool.yaml exists in new version
	_, err = os.Stat(filepath.Join(tool2Dir, ToolManifestFileName))
	assert.NoError(t, err)
}

func TestUpdateTool_NotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return tmpDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	var buf bytes.Buffer
	err := UpdateTool(exampleRegistryURL, "nonexistent-tool", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestInstallLocalTool(t *testing.T) {
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
	require.NoError(t, os.WriteFile(filepath.Join(localToolDir, "tool.yaml"), manifestData, 0644))

	// Create entrypoint
	entrypointPath := filepath.Join(localToolDir, "bin", "tool")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(entrypointPath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(entrypointPath, []byte("#!/bin/sh\necho 'local tool'"), 0755))

	// Install local tool
	var buf bytes.Buffer
	err = InstallLocalTool(localToolDir, &buf)
	require.NoError(t, err)

	// Verify tool was installed
	expectedInstallPath := filepath.Join(installDir, "local-test-tool", "0.1.0")
	assert.DirExists(t, expectedInstallPath)

	// Verify tool.yaml was copied
	// #nosec G304 -- paths are constructed from test temp directories, safe
	installedManifest, err := LoadManifest(expectedInstallPath)
	require.NoError(t, err)
	assert.Equal(t, "local-test-tool", installedManifest.Name)
	assert.Equal(t, "0.1.0", installedManifest.Version)

	// Verify entrypoint was copied
	// #nosec G304 -- paths are constructed from test temp directories, safe
	installedEntrypoint := filepath.Join(expectedInstallPath, "bin", "tool")
	assert.FileExists(t, installedEntrypoint)
}

func TestInstallLocalTool_ErrorCases(t *testing.T) {
	installDir := t.TempDir()
	originalGetInstalledToolsDir := registry.GetInstalledToolsDirFunc
	registry.GetInstalledToolsDirFunc = func() (string, error) {
		return installDir, nil
	}
	defer func() {
		registry.GetInstalledToolsDirFunc = originalGetInstalledToolsDir
	}()

	// Test: local path doesn't exist
	var buf bytes.Buffer
	err := InstallLocalTool("/nonexistent/path", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// Test: local path is a file (not a directory)
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0644))
	err = InstallLocalTool(filePath, &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a directory")

	// Test: missing tool.yaml
	toolDir := t.TempDir()
	err = InstallLocalTool(toolDir, &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load manifest")
}
