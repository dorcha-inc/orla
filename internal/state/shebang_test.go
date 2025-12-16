package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseShebangFromRoot tests the ParseShebangFromRoot function's
// primary functionality, i.e. parsing the shebang line to determine the interpreter.
func TestParseShebangFromRoot(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name            string
		fileContent     string
		wantInterpreter string
		wantErr         error
	}{
		{
			name:            "valid python shebang",
			fileContent:     "#!/usr/bin/python3\n",
			wantInterpreter: "/usr/bin/python3",
			wantErr:         nil,
		},
		{
			name:            "valid bash shebang",
			fileContent:     "#!/bin/bash\n",
			wantInterpreter: "/bin/bash",
			wantErr:         nil,
		},
		{
			name:            "valid shebang with env",
			fileContent:     "#!/usr/bin/env python\n",
			wantInterpreter: "/usr/bin/env",
			wantErr:         nil,
		},
		{
			name:            "valid shebang with env and args",
			fileContent:     "#!/usr/bin/env python3 -u\n",
			wantInterpreter: "/usr/bin/env",
			wantErr:         nil,
		},
		{
			name:            "valid shebang with spaces",
			fileContent:     "  #!/bin/sh  \n",
			wantInterpreter: "/bin/sh",
			wantErr:         nil,
		},
		{
			name:            "empty file",
			fileContent:     "",
			wantInterpreter: "",
			wantErr:         &ShebangInvalidPrefixError{},
		},
		{
			name:            "no shebang prefix",
			fileContent:     "echo hello\n",
			wantInterpreter: "",
			wantErr:         &ShebangInvalidPrefixError{},
		},
		{
			name:            "only shebang prefix",
			fileContent:     "#!\n",
			wantInterpreter: "",
			wantErr:         &ShebangIncorrectFieldCountError{},
		},
		{
			name:            "binary-like content",
			fileContent:     "\x00\x01\x02\x03",
			wantInterpreter: "",
			wantErr:         &ShebangInvalidPrefixError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, "test-"+tt.name)
			// #nosec G306 -- test file permissions are acceptable for temporary test files
			err := os.WriteFile(testFile, []byte(tt.fileContent), 0644)
			require.NoError(t, err, "Failed to create test file")

			// Open directory as root and parse shebang
			root, err := os.OpenRoot(tmpDir)
			require.NoError(t, err)
			defer core.LogDeferredError(root.Close)

			fileName := filepath.Base(testFile)
			interpreter, err := ParseShebangFromRoot(root, fileName)

			// Check interpreter
			assert.Equal(t, tt.wantInterpreter, interpreter, "Interpreter should match")

			// Check error type
			if tt.wantErr == nil {
				assert.NoError(t, err, "Should not return error")
			} else {
				require.Error(t, err, "Should return error")
				var e *ShebangFileReadError
				if errors.As(err, &e) {
					assert.ErrorAs(t, err, &e, "Should be ShebangFileReadError")
				} else {
					var fieldCountErr *ShebangIncorrectFieldCountError
					if errors.As(err, &fieldCountErr) {
						assert.ErrorAs(t, err, &fieldCountErr, "Should be ShebangIncorrectFieldCountError")
					} else {
						var prefixErr *ShebangInvalidPrefixError
						if errors.As(err, &prefixErr) {
							assert.ErrorAs(t, err, &prefixErr, "Should be ShebangInvalidPrefixError")
						}
					}
				}
			}
		})
	}
}

// TestParseShebangFromRoot_NonExistentFile tests the ParseShebangFromRoot function's
// error handling for a non-existent file.
func TestParseShebangFromRoot_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	root, err := os.OpenRoot(tmpDir)
	require.NoError(t, err)
	defer core.LogDeferredError(root.Close)

	interpreter, err := ParseShebangFromRoot(root, "nonexistent-file")

	assert.Empty(t, interpreter, "Interpreter should be empty for non-existent file")
	require.Error(t, err, "Should return error for non-existent file")
	var fileReadErr *ShebangFileReadError
	assert.ErrorAs(t, err, &fileReadErr, "Should be ShebangFileReadError")
}

// TestShebangErrorTypes tests the error types for the shebang parsing functions.
// Essentially, we want to make sure the error types are sent out with enough information
// to be able to debug the issue.
func TestShebangErrorTypes(t *testing.T) {
	t.Run("ShebangFileReadError", func(t *testing.T) {
		err := NewShebangFileReadError("/test/path")
		assert.NotEmpty(t, err.Error(), "Error message should not be empty")
		assert.Equal(t, "/test/path", err.Path, "Path should match")
	})

	t.Run("ShebangIncorrectFieldCountError", func(t *testing.T) {
		err := NewShebangIncorrectFieldCountError("/test/path", "test line", 3)
		assert.NotEmpty(t, err.Error(), "Error message should not be empty")
		assert.Equal(t, "/test/path", err.Path, "Path should match")
		assert.Equal(t, 3, err.Count, "Count should match")
	})

	t.Run("ShebangInvalidPrefixError", func(t *testing.T) {
		err := NewShebangInvalidPrefixError("/test/path", "test line")
		assert.NotEmpty(t, err.Error(), "Error message should not be empty")
		assert.Equal(t, "/test/path", err.Path, "Path should match")
	})
}
