package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const exampleRegistryURL = "https://example.com/registry"

func TestFindTool(t *testing.T) {
	registry := &RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []ToolEntry{
			{
				Name:        "fs",
				Description: "Filesystem tool",
				Repository:  "https://example.com/orla-tool-fs",
			},
			{
				Name:        "http",
				Description: "HTTP tool",
				Repository:  "https://example.com/orla-tool-http",
			},
		},
	}

	// Test finding existing tool
	tool, err := FindTool(registry, "fs")
	require.NoError(t, err)
	assert.Equal(t, "fs", tool.Name)
	assert.Equal(t, "Filesystem tool", tool.Description)

	// Test finding non-existent tool
	_, err = FindTool(registry, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSuggestSimilarToolName(t *testing.T) {
	registry := &RegistryIndex{
		Tools: []ToolEntry{
			{Name: "fs"},
			{Name: "http"},
			{Name: "git"},
		},
	}

	// Test typo detection
	suggestion := SuggestSimilarToolName(registry, "fss")
	assert.Equal(t, "fs", suggestion)

	// Test no suggestions for completely different name
	suggestion = SuggestSimilarToolName(registry, "xyz")
	assert.Empty(t, suggestion)
}

func TestResolveVersion(t *testing.T) {
	tool := &ToolEntry{
		Name:       "fs",
		Repository: "https://example.com/orla-tool-fs",
	}

	// Mock git runner to return tags
	mockRunner := &MockGitRunner{
		ListTagsFunc: func(repoURL string) ([]string, error) {
			return []string{"v0.1.0", "v0.2.0", "v0.3.0-beta"}, nil
		},
	}
	originalRunner := defaultGitRunner
	defaultGitRunner = mockRunner
	defer func() { defaultGitRunner = originalRunner }()

	// Test latest (should return latest stable)
	tag, err := ResolveVersion(tool, "latest")
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", tag)

	// Test empty constraint (should return latest stable)
	tag, err = ResolveVersion(tool, "")
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", tag)

	// Test exact tag
	tag, err = ResolveVersion(tool, "v0.1.0")
	require.NoError(t, err)
	assert.Equal(t, "v0.1.0", tag)

	// Test tag without 'v' prefix (should fail)
	_, err = ResolveVersion(tool, "0.1.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must start with 'v'")
}

func TestResolveVersion_OnlyPreRelease(t *testing.T) {
	// Test tool with only pre-release versions
	tool := &ToolEntry{
		Name:       "fs",
		Repository: "https://example.com/orla-tool-fs",
	}

	mockRunner := &MockGitRunner{
		ListTagsFunc: func(repoURL string) ([]string, error) {
			return []string{"v0.1.0-alpha", "v0.2.0-beta", "v0.3.0-rc"}, nil
		},
	}
	originalRunner := defaultGitRunner
	defaultGitRunner = mockRunner
	defer func() { defaultGitRunner = originalRunner }()

	// Should return the latest pre-release when no stable versions exist
	tag, err := ResolveVersion(tool, "latest")
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0-rc", tag)
}

func TestResolveVersion_NoVersions(t *testing.T) {
	tool := &ToolEntry{
		Name:       "fs",
		Repository: "https://example.com/orla-tool-fs",
	}

	mockRunner := &MockGitRunner{
		ListTagsFunc: func(repoURL string) ([]string, error) {
			return []string{}, nil
		},
	}
	originalRunner := defaultGitRunner
	defaultGitRunner = mockRunner
	defer func() { defaultGitRunner = originalRunner }()

	_, err := ResolveVersion(tool, "latest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no tags found")
}

func TestResolveVersion_MixedVersions(t *testing.T) {
	// Test with mixed stable and pre-release versions
	tool := &ToolEntry{
		Name:       "fs",
		Repository: "https://example.com/orla-tool-fs",
	}

	mockRunner := &MockGitRunner{
		ListTagsFunc: func(repoURL string) ([]string, error) {
			return []string{"v0.1.0", "v0.2.0-alpha", "v0.3.0", "v0.4.0-beta"}, nil
		},
	}
	originalRunner := defaultGitRunner
	defaultGitRunner = mockRunner
	defer func() { defaultGitRunner = originalRunner }()

	// Should return latest stable version (v0.3.0), not pre-release
	tag, err := ResolveVersion(tool, "latest")
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0", tag)
}

func TestSanitizeURLForCache(t *testing.T) {
	// Test that the function returns a filesystem-safe key
	key, err := sanitizeURLForCache("https://github.com/user/repo")
	require.NoError(t, err)
	assert.NotContains(t, key, "/")
	assert.NotContains(t, key, ":")
	assert.NotEmpty(t, key)
	// SHA256 produces 64 hex characters
	assert.Len(t, key, 64)

	// Test that same URL produces same key
	key2, err := sanitizeURLForCache("https://github.com/user/repo")
	require.NoError(t, err)
	assert.Equal(t, key, key2)

	// Test that different URLs produce different keys
	key3, err := sanitizeURLForCache("https://github.com/user/other-repo")
	require.NoError(t, err)
	assert.NotEqual(t, key, key3)

	// Test with URL that has various components
	key4, err := sanitizeURLForCache("http://example.com:8080/path?query=value#fragment")
	require.NoError(t, err)
	assert.NotEmpty(t, key4)
	assert.Len(t, key4, 64)

	// Test with URL missing scheme (should return error)
	_, err = sanitizeURLForCache("example.com/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing scheme")

	// Test with URL missing host (should return error)
	_, err = sanitizeURLForCache("http:///path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing host")
}

func TestSanitizeURLForCache_Exported(t *testing.T) {
	// Test the exported function (wrapper around sanitizeURLForCache)
	key, err := SanitizeURLForCache("https://github.com/user/repo")
	require.NoError(t, err)
	assert.NotEmpty(t, key)
	assert.Len(t, key, 64)

	// Test error case
	_, err = SanitizeURLForCache("invalid-url")
	assert.Error(t, err)
}

func TestLoadCachedRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "registry.yaml")

	// Create a test registry file
	index := &RegistryIndex{
		Version:     1,
		RegistryURL: "https://example.com/registry",
		Tools: []ToolEntry{
			{Name: "fs", Description: "Filesystem tool"},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Load cached registry
	cached, err := loadCachedRegistry(cachePath)
	require.NoError(t, err)
	assert.Equal(t, 1, cached.Version)
	assert.Len(t, cached.Tools, 1)
	assert.Equal(t, "fs", cached.Tools[0].Name)
}

func TestLoadCachedRegistry_Expired(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "registry.yaml")

	// Create a test registry file
	index := &RegistryIndex{
		Version:     1,
		RegistryURL: "https://example.com/registry",
		Tools: []ToolEntry{
			{Name: "fs", Description: "Filesystem tool"},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Modify file time to be older than 1 hour
	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(cachePath, oldTime, oldTime))

	// Load cached registry should fail due to expiration
	_, err = loadCachedRegistry(cachePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache expired")
}

func TestLoadCachedRegistry_Nonexistent(t *testing.T) {
	_, err := loadCachedRegistry("/nonexistent/path/registry.yaml")
	assert.Error(t, err)
}

func TestSaveCachedRegistry(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "registry.yaml")

	index := &RegistryIndex{
		Version:     1,
		RegistryURL: "https://example.com/registry",
		Tools: []ToolEntry{
			{Name: "fs", Description: "Filesystem tool"},
		},
	}

	// Save cached registry
	err := saveCachedRegistry(cachePath, index)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(cachePath)
	require.NoError(t, err)

	// Load it back and verify
	cached, err := loadCachedRegistry(cachePath)
	require.NoError(t, err)
	assert.Equal(t, 1, cached.Version)
	assert.Len(t, cached.Tools, 1)
	assert.Equal(t, "fs", cached.Tools[0].Name)
}

func TestSaveCachedRegistry_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "subdir", "registry.yaml")

	index := &RegistryIndex{
		Version:     1,
		RegistryURL: "https://example.com/registry",
		Tools: []ToolEntry{
			{Name: "fs", Description: "Filesystem tool"},
		},
	}

	// Save cached registry (should create directory)
	err := saveCachedRegistry(cachePath, index)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(cachePath)
	require.NoError(t, err)
}

func TestFetchRegistry_WithCache(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create a cached registry file
	cacheKey, err := sanitizeURLForCache("https://example.com/registry")
	require.NoError(t, err)
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))

	index := &RegistryIndex{
		Version:     1,
		RegistryURL: "https://example.com/registry",
		Tools: []ToolEntry{
			{Name: "fs", Description: "Filesystem tool"},
		},
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GetRegistryCacheDir to return our test cache directory
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry with cache enabled - should use cached version
	reg, err := FetchRegistry("https://example.com/registry", true)
	require.NoError(t, err)
	assert.Equal(t, 1, reg.Version)
	assert.Len(t, reg.Tools, 1)
	assert.Equal(t, "fs", reg.Tools[0].Name)
}

func TestFetchRegistry_CloneSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	cacheKey, err := sanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create mock GitRunner that creates the registry.yaml file when cloning
	mockRunner := &MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Simulate successful clone by creating the registry.yaml file
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(registryRepoPath, 0755); err != nil {
				return err
			}
			index := &RegistryIndex{
				Version:     1,
				RegistryURL: exampleRegistryURL,
				Tools: []ToolEntry{
					{Name: "test-tool", Description: "Test tool"},
				},
			}
			data, err := yaml.Marshal(index)
			if err != nil {
				return err
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(registryYAMLPath, data, 0644)
		},
	}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry - should clone (since repo doesn't exist)
	reg, err := FetchRegistry(exampleRegistryURL, false)
	require.NoError(t, err)
	assert.Equal(t, 1, reg.Version)
	assert.Len(t, reg.Tools, 1)
	assert.Equal(t, "test-tool", reg.Tools[0].Name)
	assert.Len(t, mockRunner.CloneCalls, 1)
	assert.Equal(t, exampleRegistryURL, mockRunner.CloneCalls[0].URL)
}

func TestFetchRegistry_CloneError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Create mock GitRunner that returns an error
	mockRunner := &MockGitRunner{
		CloneErr: fmt.Errorf("git clone failed"),
	}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry should fail
	_, err := FetchRegistry(exampleRegistryURL, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to clone registry repository")
}

func TestFetchRegistry_PullSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	cacheKey, err := sanitizeURLForCache(exampleRegistryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create existing repo directory (simulating repo already exists)
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	index := &RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []ToolEntry{
			{Name: "updated-tool", Description: "Updated tool"},
		},
	}
	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create mock GitRunner
	mockRunner := &MockGitRunner{}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry - should pull (since repo exists)
	reg, err := FetchRegistry(exampleRegistryURL, false)
	require.NoError(t, err)
	assert.Equal(t, 1, reg.Version)
	assert.Len(t, reg.Tools, 1)
	assert.Equal(t, "updated-tool", reg.Tools[0].Name)
	assert.Len(t, mockRunner.PullCalls, 1)
	assert.Equal(t, registryRepoPath, mockRunner.PullCalls[0])
	assert.Len(t, mockRunner.CloneCalls, 0) // Should not clone if pull succeeds
}

func TestFetchRegistry_PullErrorThenClone(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	registryURL := "https://example.com/registry"
	cacheKey, err := sanitizeURLForCache(registryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create existing repo directory
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))
	index := &RegistryIndex{
		Version:     1,
		RegistryURL: registryURL,
		Tools: []ToolEntry{
			{Name: "cloned-tool", Description: "Cloned tool"},
		},
	}
	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create mock GitRunner that fails on pull but succeeds on clone
	mockRunner := &MockGitRunner{
		PullErr: fmt.Errorf("git pull failed"),
		CloneFunc: func(url, targetPath string) error {
			// Simulate successful clone by creating the registry.yaml file
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(registryRepoPath, 0755); err != nil {
				return err
			}
			index := &RegistryIndex{
				Version:     1,
				RegistryURL: registryURL,
				Tools: []ToolEntry{
					{Name: "cloned-tool", Description: "Cloned tool"},
				},
			}
			data, err := yaml.Marshal(index)
			if err != nil {
				return err
			}
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(registryYAMLPath, data, 0644)
		},
	}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry - should pull, fail, then clone
	reg, err := FetchRegistry(registryURL, false)
	require.NoError(t, err)
	assert.Equal(t, 1, reg.Version)
	assert.Len(t, reg.Tools, 1)
	assert.Equal(t, "cloned-tool", reg.Tools[0].Name)
	assert.Len(t, mockRunner.PullCalls, 1)
	assert.Len(t, mockRunner.CloneCalls, 1) // Should clone after pull fails
}

func TestCloneRegistry(t *testing.T) {
	mockRunner := &MockGitRunner{}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	err := cloneRegistry("https://example.com/repo", "/tmp/target")
	require.NoError(t, err)
	assert.Len(t, mockRunner.CloneCalls, 1)
	assert.Equal(t, "https://example.com/repo", mockRunner.CloneCalls[0].URL)
	assert.Equal(t, "/tmp/target", mockRunner.CloneCalls[0].TargetPath)
}

func TestFetchRegistry_GetCacheDirError(t *testing.T) {
	// Mock GetRegistryCacheDir to return an error
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return "", fmt.Errorf("failed to get cache dir")
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	_, err := FetchRegistry(exampleRegistryURL, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get cache directory")
}

func TestFetchRegistry_InvalidURL(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Test with invalid URL (missing scheme)
	_, err := FetchRegistry("example.com/registry", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid registry URL")
}

func TestFetchRegistry_ReadFileError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	registryURL := exampleRegistryURL
	cacheKey, err := sanitizeURLForCache(registryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	// Create repo directory but don't create registry.yaml

	// Create mock GitRunner
	mockRunner := &MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// Simulate successful clone but don't create registry.yaml
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			return os.MkdirAll(registryRepoPath, 0755)
		},
	}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry should fail when reading registry.yaml
	_, err = FetchRegistry(registryURL, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read registry.yaml")
}

func TestFetchRegistry_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	registryURL := exampleRegistryURL
	cacheKey, err := sanitizeURLForCache(registryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Create mock GitRunner that creates invalid YAML
	mockRunner := &MockGitRunner{
		CloneFunc: func(url, targetPath string) error {
			// #nosec G301 -- test directory permissions are acceptable for temporary test files
			if err := os.MkdirAll(registryRepoPath, 0755); err != nil {
				return err
			}
			// Create invalid YAML
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			return os.WriteFile(registryYAMLPath, []byte("invalid: yaml: [unclosed"), 0644)
		},
	}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// Fetch registry should fail when parsing invalid YAML
	_, err = FetchRegistry(registryURL, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse registry.yaml")
}

func TestFetchRegistry_RemoveAllError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	registryURL := exampleRegistryURL
	cacheKey, err := sanitizeURLForCache(registryURL)
	require.NoError(t, err)
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))

	// Create mock GitRunner that fails on pull
	mockRunner := &MockGitRunner{
		PullErr:  fmt.Errorf("git pull failed"),
		CloneErr: fmt.Errorf("clone also fails"),
	}
	originalRunner := defaultGitRunner
	SetGitRunner(mockRunner)
	defer SetGitRunner(originalRunner)

	// Mock GetRegistryCacheDir
	originalGetCacheDir := getRegistryCacheDirFunc
	getRegistryCacheDirFunc = func() (string, error) {
		return cacheDir, nil
	}
	defer func() {
		getRegistryCacheDirFunc = originalGetCacheDir
	}()

	// On Unix systems, RemoveAll rarely fails, but we can test the path exists
	// The actual error would come from clone failing after remove
	_, err = FetchRegistry(registryURL, false)
	assert.Error(t, err)
	// Should fail on clone after pull fails
	assert.Contains(t, err.Error(), "failed to clone registry repository")
}

func TestSuggestSimilarToolName_EmptyRegistry(t *testing.T) {
	registry := &RegistryIndex{
		Tools: []ToolEntry{},
	}

	suggestion := SuggestSimilarToolName(registry, "test")
	assert.Empty(t, suggestion)
}

func TestGetOrlaHomeDir_Error(t *testing.T) {
	// This is hard to test without mocking os.UserHomeDir
	// We can't easily simulate this error, but the code path exists
	// For now, we'll document that this is tested via integration tests
	t.Skip("os.UserHomeDir() error is difficult to simulate in unit tests")
}

func TestSaveCachedRegistry_MkdirAllError(t *testing.T) {
	// Create a path that would fail MkdirAll
	// On Unix, this is hard to simulate, but we can test the error path exists
	tmpDir := t.TempDir()
	// Create a file instead of directory to make MkdirAll fail
	cacheFile := filepath.Join(tmpDir, "cache-file")
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cacheFile, []byte("test"), 0644))
	cachePath := filepath.Join(cacheFile, "registry.yaml")

	index := &RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []ToolEntry{
			{Name: "fs", Description: "Filesystem tool"},
		},
	}

	// Save should fail because cacheFile is a file, not a directory
	err := saveCachedRegistry(cachePath, index)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create cache directory")
}

func TestSearchTools(t *testing.T) {
	registry := &RegistryIndex{
		Version:     1,
		RegistryURL: exampleRegistryURL,
		Tools: []ToolEntry{
			{
				Name:        "filesystem",
				Description: "Access local filesystem",
				Keywords:    []string{"fs", "file", "read"},
			},
			{
				Name:        "http-client",
				Description: "Make HTTP requests",
				Keywords:    []string{"http", "network", "request"},
			},
			{
				Name:        "database",
				Description: "Database operations",
				Keywords:    []string{"db", "sql"},
			},
		},
	}

	// Test search by name
	results := SearchTools(registry, "filesystem")
	assert.Len(t, results, 1)
	assert.Equal(t, "filesystem", results[0].Name)

	// Test search by description
	results = SearchTools(registry, "HTTP")
	assert.Len(t, results, 1)
	assert.Equal(t, "http-client", results[0].Name)

	// Test search by keyword
	results = SearchTools(registry, "file")
	assert.Len(t, results, 1)
	assert.Equal(t, "filesystem", results[0].Name)

	// Test case-insensitive search
	results = SearchTools(registry, "DATABASE")
	assert.Len(t, results, 1)
	assert.Equal(t, "database", results[0].Name)

	// Test partial match
	results = SearchTools(registry, "http")
	assert.Len(t, results, 1)
	assert.Equal(t, "http-client", results[0].Name)

	// Test no matches
	results = SearchTools(registry, "nonexistent")
	assert.Empty(t, results)

	// Test empty query (should return all)
	results = SearchTools(registry, "")
	assert.Len(t, results, 3)
}

func TestExtractVersionFromDir(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		setup       func() (toolDir string, installDir string)
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid path",
			setup: func() (string, string) {
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolDir := filepath.Join(installDir, "fs", "0.1.0")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(toolDir, 0755))
				return toolDir, installDir
			},
			want:    "0.1.0",
			wantErr: false,
		},
		{
			name: "path with less than 2 parts",
			setup: func() (string, string) {
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolDir := filepath.Join(installDir, "fs")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(toolDir, 0755))
				return toolDir, installDir
			},
			want:        "",
			wantErr:     true,
			errContains: "invalid tool directory path structure",
		},
		{
			name: "nested path",
			setup: func() (string, string) {
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolDir := filepath.Join(installDir, "fs", "0.1.0", "bin")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(toolDir, 0755))
				return toolDir, installDir
			},
			want:    "0.1.0",
			wantErr: false,
		},
		{
			name: "different version format",
			setup: func() (string, string) {
				installDir := filepath.Join(tmpDir, "tools")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(installDir, 0755))
				toolDir := filepath.Join(installDir, "tool", "1.2.3-beta")
				// #nosec G301 -- test directory permissions are acceptable for temporary test files
				require.NoError(t, os.MkdirAll(toolDir, 0755))
				return toolDir, installDir
			},
			want:    "1.2.3-beta",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolDir, installDir := tt.setup()
			got, err := ExtractVersionFromDir(toolDir, installDir)
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
