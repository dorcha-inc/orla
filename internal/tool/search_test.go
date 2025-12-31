package tool

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSearchTools_EmptyResults_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "test-tool", Description: "A test tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("nonexistent", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No tools found matching 'nonexistent'")
}

func TestSearchTools_EmptyResults_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "test-tool", Description: "A test tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("nonexistent", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		JSON:        true,
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Equal(t, "[]", output)
}

func TestSearchTools_SingleResult_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "fs-tool", Description: "Filesystem operations tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("fs", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "fs-tool: Filesystem operations tool")
	assert.Contains(t, output, "Install a tool with: orla tool install TOOL-NAME")
}

func TestSearchTools_MultipleResults_Simple(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "fs-tool", Description: "Filesystem operations tool"},
		{Name: "http-tool", Description: "HTTP client tool"},
		{Name: "db-tool", Description: "Database operations tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("tool", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "fs-tool: Filesystem operations tool")
	assert.Contains(t, output, "http-tool: HTTP client tool")
	assert.Contains(t, output, "db-tool: Database operations tool")
	assert.Contains(t, output, "Install a tool with: orla tool install TOOL-NAME")
}

func TestSearchTools_Verbose(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "fs-tool", Description: "Filesystem operations tool"},
		{Name: "http-tool", Description: "HTTP client tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("tool", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Verbose:     true,
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.GreaterOrEqual(t, len(lines), 3) // Header, separator, and at least one tool
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "DESCRIPTION")
	assert.Contains(t, output, "fs-tool")
	assert.Contains(t, output, "Filesystem operations tool")
	assert.Contains(t, output, "Install a tool with: orla tool install TOOL-NAME")
}

func TestSearchTools_Verbose_LongDescription(t *testing.T) {
	tmpDir := t.TempDir()
	longDescription := strings.Repeat("This is a very long description. ", 5) // Should be > 60 chars
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "test-tool", Description: longDescription},
	})

	var buf bytes.Buffer
	err := SearchTools("test", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Verbose:     true,
		Writer:      &buf,
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
			if len(parts) >= 2 {
				description := parts[1]
				assert.LessOrEqual(t, len(description), 60)
				assert.True(t, strings.HasSuffix(description, "...") || len(description) <= 60)
			}
		}
	}
}

func TestSearchTools_Simple_LongDescription(t *testing.T) {
	tmpDir := t.TempDir()
	longDescription := strings.Repeat("This is a very long description. ", 5) // Should be > 80 chars
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "test-tool", Description: longDescription},
	})

	var buf bytes.Buffer
	err := SearchTools("test", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	// Description should be truncated to 80 chars with "..."
	assert.Contains(t, output, "...")
	// Verify the format is "tool-name: description"
	assert.Contains(t, output, "test-tool:")
}

func TestSearchTools_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "fs-tool", Description: "Filesystem operations tool"},
		{Name: "http-tool", Description: "HTTP client tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("tool", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		JSON:        true,
		Writer:      &buf,
	})
	require.NoError(t, err)

	var results []registry.ToolEntry
	err = json.Unmarshal(buf.Bytes(), &results)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify tool data
	toolNames := make(map[string]bool)
	for _, tool := range results {
		toolNames[tool.Name] = true
		switch tool.Name {
		case "fs-tool":
			assert.Equal(t, "Filesystem operations tool", tool.Description)
		case "http-tool":
			assert.Equal(t, "HTTP client tool", tool.Description)
		}
	}
	assert.True(t, toolNames["fs-tool"])
	assert.True(t, toolNames["http-tool"])
}

func TestSearchTools_JSON_NoInstallMessage(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "fs-tool", Description: "Filesystem operations tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("fs", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		JSON:        true,
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	// JSON output should not contain the install message
	assert.NotContains(t, output, "Install a tool with: orla tool install TOOL-NAME")
}

func TestSearchTools_Error_FetchRegistry(t *testing.T) {
	var buf bytes.Buffer
	err := SearchTools("test", SearchOptions{
		RegistryURL: "invalid://registry-url",
		Writer:      &buf,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch registry")
}

func TestSearchTools_Keywords(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{
			Name:        "fs-tool",
			Description: "Filesystem tool",
			Keywords:    []string{"filesystem", "fs", "disk"},
		},
		{
			Name:        "http-tool",
			Description: "HTTP tool",
			Keywords:    []string{"http", "web", "api"},
		},
	})

	var buf bytes.Buffer
	err := SearchTools("filesystem", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "fs-tool")
	assert.NotContains(t, output, "http-tool")
}

func TestSearchTools_CaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	setupTestRegistry(t, tmpDir, []registry.ToolEntry{
		{Name: "fs-tool", Description: "Filesystem operations tool"},
	})

	var buf bytes.Buffer
	err := SearchTools("FS-TOOL", SearchOptions{
		RegistryURL: getTestRegistryURL(),
		Writer:      &buf,
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "fs-tool")
}

// Helper functions

func setupTestRegistry(t *testing.T, tmpDir string, tools []registry.ToolEntry) {
	cacheDir := filepath.Join(tmpDir, "cache")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	registryURL := "https://example.com/test-registry"
	cacheKey, err := registry.SanitizeURLForCache(registryURL)
	require.NoError(t, err)

	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(registryRepoPath, 0755))

	index := &registry.RegistryIndex{
		Version:     1,
		RegistryURL: registryURL,
		Tools:       tools,
	}

	data, err := yaml.Marshal(index)
	require.NoError(t, err)
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(registryYAMLPath, data, 0644))

	// Create cached registry file
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")
	// #nosec G301 -- test directory permissions are acceptable for temporary test files
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0755))
	// #nosec G306 -- test file permissions are acceptable for temporary test files
	require.NoError(t, os.WriteFile(cachePath, data, 0644))

	// Mock GetRegistryCacheDir
	if registry.GetRegistryCacheDirFunc != nil {
		originalGetCacheDir := *registry.GetRegistryCacheDirFunc
		*registry.GetRegistryCacheDirFunc = func() (string, error) {
			return cacheDir, nil
		}
		t.Cleanup(func() {
			if registry.GetRegistryCacheDirFunc != nil {
				*registry.GetRegistryCacheDirFunc = originalGetCacheDir
			}
		})
	}
}

func getTestRegistryURL() string {
	// Return a registry URL that will use the cached registry we set up
	return "https://example.com/test-registry"
}
