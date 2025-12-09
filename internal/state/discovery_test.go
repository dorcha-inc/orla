package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanToolsFromDirectory tests the ScanToolsFromDirectory function's
// primary functionality, i.e. scanning the tools directory for executable files.
func TestScanToolsFromDirectory(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create test files with different shebangs
	testFiles := map[string]string{
		"python-tool.py": "#!/usr/bin/python3\nprint('hello')",
		"bash-tool.sh":   "#!/bin/bash\necho hello",
		"sh-tool.sh":     "#!/bin/sh\necho test",
		"binary-tool":    "\x00\x01\x02\x03", // Binary content
		"no-shebang.txt": "just some text",
		"empty-file":     "",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(tmpDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0755)
		require.NoError(t, err, "Failed to create test file %s", filename)
	}

	// Create a subdirectory (should be ignored)
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	require.NoError(t, err, "Failed to create subdirectory")

	// Scan the directory
	tools, err := ScanToolsFromDirectory(tmpDir)
	require.NoError(t, err, "Should scan directory without error")

	// Verify expected tools are found
	expectedTools := []string{"python-tool", "bash-tool", "sh-tool", "binary-tool", "no-shebang", "empty-file"}
	assert.Len(t, tools, len(expectedTools), "Should find expected number of tools")

	// Check each expected tool
	for _, toolName := range expectedTools {
		tool, ok := tools[toolName]
		require.True(t, ok, "Should find tool: %s", toolName)
		assert.Equal(t, toolName, tool.Name, "Tool name should match")

		expectedPath := filepath.Join(tmpDir, toolName+filepath.Ext(tool.Path))
		assert.True(t, tool.Path == expectedPath || tool.Path == filepath.Join(tmpDir, toolName),
			"Tool path should contain %s", toolName)
	}

	// Verify interpreters are parsed correctly
	assert.Equal(t, "/usr/bin/python3", tools["python-tool"].Interpreter, "Python tool interpreter should match")
	assert.Equal(t, "/bin/bash", tools["bash-tool"].Interpreter, "Bash tool interpreter should match")
	assert.Equal(t, "/bin/sh", tools["sh-tool"].Interpreter, "Sh tool interpreter should match")

	// Binary and non-shebang files should have empty interpreter
	assert.Empty(t, tools["binary-tool"].Interpreter, "Binary tool should have empty interpreter")
	assert.Empty(t, tools["no-shebang"].Interpreter, "Non-shebang file should have empty interpreter")
}

// TestScanToolsFromDirectory_NonExistentDirectory tests the ScanToolsFromDirectory function's
// error handling for a directory that does not exist.
func TestScanToolsFromDirectory_NonExistentDirectory(t *testing.T) {
	nonExistentDir := "/nonexistent/directory/path"
	_, err := ScanToolsFromDirectory(nonExistentDir)

	assert.Error(t, err, "Should return error for non-existent directory")
}

// TestScanToolsFromDirectory_EmptyDirectory tests the ScanToolsFromDirectory function's
// functionality for an empty directory.
func TestScanToolsFromDirectory_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	tools, err := ScanToolsFromDirectory(tmpDir)
	require.NoError(t, err, "Should scan empty directory without error")
	assert.Empty(t, tools, "Should find no tools in empty directory")
}

// TestScanToolsFromDirectory_ToolNameExtraction tests the ScanToolsFromDirectory function's
// functionality for extracting the tool name from the file name. While we also test this
// for simpler cases in the TestScanToolsFromDirectory function, we want to test this
// in a more comprehensive way here.
func TestScanToolsFromDirectory_ToolNameExtraction(t *testing.T) {
	tmpDir := t.TempDir()

	testCases := []struct {
		filename string
		wantName string
		content  string
	}{
		{"tool.sh", "tool", "#!/bin/bash"},
		{"my-tool.py", "my-tool", "#!/usr/bin/python3"},
		{"complex.tool.name.js", "complex.tool.name", "#!/usr/bin/node"},
		{"noextension", "noextension", "#!/bin/sh"},
	}

	for _, tc := range testCases {
		filePath := filepath.Join(tmpDir, tc.filename)
		err := os.WriteFile(filePath, []byte(tc.content), 0755)
		require.NoError(t, err, "Failed to create test file %s", tc.filename)
	}

	tools, err := ScanToolsFromDirectory(tmpDir)
	require.NoError(t, err, "Should scan directory without error")

	for _, tc := range testCases {
		tool, ok := tools[tc.wantName]
		require.True(t, ok, "Should find tool: %s", tc.wantName)
		assert.Equal(t, tc.wantName, tool.Name, "Tool name should match")
	}
}

// TestScanToolsFromDirectory_DuplicateNames tests the ScanToolsFromDirectory function's
// functionality for handling duplicate tool names.
func TestScanToolsFromDirectory_DuplicateNames(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with same name but different extensions
	files := map[string]string{
		"tool.sh": "#!/bin/bash",
		"tool.py": "#!/usr/bin/python3",
	}

	for filename, content := range files {
		filePath := filepath.Join(tmpDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0755)
		require.NoError(t, err, "Failed to create test file %s", filename)
	}

	_, err := ScanToolsFromDirectory(tmpDir)
	// We expect a DuplicateToolNameError error
	var duplicateToolNameErr *DuplicateToolNameError
	assert.ErrorAs(t, err, &duplicateToolNameErr, "Should be DuplicateToolNameError")
	assert.Equal(t, "tool", duplicateToolNameErr.Name, "Duplicate tool name should be 'tool'")

	// Test Error() method to get coverage
	assert.Contains(t, duplicateToolNameErr.Error(), "tool with name tool already exists")
}
