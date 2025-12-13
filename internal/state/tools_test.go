package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewToolNotFoundError tests the creation of a ToolNotFoundError
func TestNewToolNotFoundError(t *testing.T) {
	err := NewToolNotFoundError("test-tool")
	require.NotNil(t, err)
	assert.Equal(t, "test-tool", err.Name)
}

// TestToolNotFoundError_Error tests the error message formatting
func TestToolNotFoundError_Error(t *testing.T) {
	err := NewToolNotFoundError("test-tool")
	require.NotNil(t, err)
	assert.Equal(t, "tool not found: test-tool", err.Error())
}

// TestNewToolsRegistry tests the creation of a new tools registry
func TestNewToolsRegistry(t *testing.T) {
	registry := NewToolsRegistry()
	require.NotNil(t, registry)
	assert.NotNil(t, registry.Tools)
	assert.Equal(t, 0, len(registry.Tools))
}

// TestToolsRegistry_AddTool tests adding a tool to the registry
func TestToolsRegistry_AddTool(t *testing.T) {
	registry := NewToolsRegistry()

	tool := &ToolEntry{
		Name:        "test-tool",
		Description: "A test tool",
		Path:        "/path/to/tool",
		Interpreter: "/bin/sh",
	}

	err := registry.AddTool(tool)
	require.NoError(t, err)

	// Verify the tool was added
	retrievedTool, err := registry.GetTool("test-tool")
	require.NoError(t, err)
	assert.Equal(t, tool, retrievedTool)
}

// TestToolsRegistry_AddTool_Duplicate tests that adding a tool with the same name returns an error
func TestToolsRegistry_AddTool_Duplicate(t *testing.T) {
	registry := NewToolsRegistry()

	tool1 := &ToolEntry{
		Name:        "test-tool",
		Description: "First tool",
		Path:        "/path/to/tool1",
		Interpreter: "",
	}

	tool2 := &ToolEntry{
		Name:        "test-tool",
		Description: "Second tool",
		Path:        "/path/to/tool2",
		Interpreter: "/bin/sh",
	}

	err := registry.AddTool(tool1)
	require.NoError(t, err)

	// Adding a duplicate tool should return an error
	err = registry.AddTool(tool2)
	assert.Error(t, err)
	var duplicateErr *DuplicateToolNameError
	assert.ErrorAs(t, err, &duplicateErr)
	assert.Equal(t, "test-tool", duplicateErr.Name)

	// Verify the first tool is still in the registry
	retrievedTool, err := registry.GetTool("test-tool")
	require.NoError(t, err)
	assert.Equal(t, tool1, retrievedTool)
	assert.Equal(t, "First tool", retrievedTool.Description)
}

// TestToolsRegistry_GetTool tests retrieving a tool from the registry
func TestToolsRegistry_GetTool(t *testing.T) {
	registry := NewToolsRegistry()

	tool := &ToolEntry{
		Name:        "test-tool",
		Description: "A test tool",
		Path:        "/path/to/tool",
		Interpreter: "",
	}

	err := registry.AddTool(tool)
	require.NoError(t, err)

	// Test successful retrieval
	retrievedTool, err := registry.GetTool("test-tool")
	require.NoError(t, err)
	assert.Equal(t, tool, retrievedTool)

	// Test retrieval of non-existent tool
	_, err = registry.GetTool("nonexistent")
	assert.Error(t, err)
	var toolNotFoundErr *ToolNotFoundError
	assert.ErrorAs(t, err, &toolNotFoundErr)
	assert.Equal(t, "nonexistent", toolNotFoundErr.Name)
}

// TestToolsRegistry_ListTools tests listing all tools in the registry
func TestToolsRegistry_ListTools(t *testing.T) {
	registry := NewToolsRegistry()

	// Initially empty
	tools := registry.ListTools()
	assert.Equal(t, 0, len(tools))

	// Add some tools
	tool1 := &ToolEntry{
		Name:        "tool1",
		Description: "First tool",
		Path:        "/path/to/tool1",
		Interpreter: "",
	}

	tool2 := &ToolEntry{
		Name:        "tool2",
		Description: "Second tool",
		Path:        "/path/to/tool2",
		Interpreter: "/bin/sh",
	}

	tool3 := &ToolEntry{
		Name:        "tool3",
		Description: "Third tool",
		Path:        "/path/to/tool3",
		Interpreter: "/usr/bin/python3",
	}

	err := registry.AddTool(tool1)
	require.NoError(t, err)
	err = registry.AddTool(tool2)
	require.NoError(t, err)
	err = registry.AddTool(tool3)
	require.NoError(t, err)

	// List tools
	tools = registry.ListTools()
	assert.Equal(t, 3, len(tools))

	// Verify all tools are present (order may vary)
	toolMap := make(map[string]*ToolEntry)
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	assert.Contains(t, toolMap, "tool1")
	assert.Contains(t, toolMap, "tool2")
	assert.Contains(t, toolMap, "tool3")
	assert.Equal(t, tool1, toolMap["tool1"])
	assert.Equal(t, tool2, toolMap["tool2"])
	assert.Equal(t, tool3, toolMap["tool3"])
}

// TestNewToolsRegistryFromDirectory tests creating a registry from a directory
func TestNewToolsRegistryFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with different shebangs
	testFiles := map[string]string{
		"python-tool.py": "#!/usr/bin/python3\nprint('hello')",
		"bash-tool.sh":   "#!/bin/bash\necho hello",
		"binary-tool":    "\x00\x01\x02\x03", // Binary content
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(tmpDir, filename)
		// #nosec G306 -- test file permissions are acceptable for temporary test files
		err := os.WriteFile(filePath, []byte(content), 0755)
		require.NoError(t, err)
	}

	registry, err := NewToolsRegistryFromDirectory(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, registry)

	// Verify tools were discovered
	tools := registry.ListTools()
	assert.GreaterOrEqual(t, len(tools), 2) // At least python-tool and bash-tool

	// Verify we can retrieve specific tools
	pythonTool, err := registry.GetTool("python-tool")
	require.NoError(t, err)
	assert.Equal(t, "python-tool", pythonTool.Name)
	assert.Contains(t, pythonTool.Interpreter, "python")

	bashTool, err := registry.GetTool("bash-tool")
	require.NoError(t, err)
	assert.Equal(t, "bash-tool", bashTool.Name)
	assert.Contains(t, bashTool.Interpreter, "bash")
}

// TestNewToolsRegistryFromDirectory_EmptyDirectory tests creating a registry from an empty directory
func TestNewToolsRegistryFromDirectory_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	registry, err := NewToolsRegistryFromDirectory(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, registry)

	tools := registry.ListTools()
	assert.Equal(t, 0, len(tools))
}

// TestNewToolsRegistryFromDirectory_NonexistentDirectory tests creating a registry from a non-existent directory
// It should return an empty registry, not an error, to allow graceful degradation
func TestNewToolsRegistryFromDirectory_NonexistentDirectory(t *testing.T) {
	registry, err := NewToolsRegistryFromDirectory("/nonexistent/directory")
	require.NoError(t, err, "Should not return error for non-existent directory (graceful degradation)")
	assert.NotNil(t, registry, "Should return a registry")
	assert.Empty(t, registry.ListTools(), "Should have no tools when directory doesn't exist")
}
