package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// DuplicateToolNameError is an error that is returned when a tool with the same name already exists
type DuplicateToolNameError struct {
	Name string `json:"name"`
}

// Error returns the error message for the DuplicateToolNameError
func (e *DuplicateToolNameError) Error() string {
	return fmt.Sprintf("tool with name %s already exists", e.Name)
}

// NewDuplicateToolNameError creates a new DuplicateToolNameError
func NewDuplicateToolNameError(name string) *DuplicateToolNameError {
	return &DuplicateToolNameError{Name: name}
}

// Interface guard for DuplicateToolNameError
// This ensures that DuplicateToolNameError implements the error interface.
var _ error = &DuplicateToolNameError{}

// ScanToolsFromDirectory scans the tools directory for executable files
func ScanToolsFromDirectory(dir string) (map[string]*ToolEntry, error) {
	toolMap := make(map[string]*ToolEntry)

	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			zap.L().Warn("Tools directory does not exist, no tools will be available",
				zap.String("directory", dir),
				zap.String("hint", "Create the directory and add executable files to enable tools"))
			return toolMap, nil // Return empty map, not an error
		}
		return nil, fmt.Errorf("failed to access tools directory %s: %w", dir, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("tools directory path is not a directory: %s", dir)
	}

	// Note(jadidbourbaki): we are using filepath.WalkDir instead of filepath.Walk as
	// it is more efficient according to the golang documentation [1].
	// [1] "Walk is less efficient than WalkDir, introduced in Go 1.16, which avoids
	// calling os.Lstat on every visited file or directory."
	// ~ https://pkg.go.dev/path/filepath#Walk
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Check if file is executable (RFC 1 section 4.4: mode & 0111 != 0)
		info, err := d.Info()
		if err != nil {
			zap.L().Warn("Failed to get file info, skipping", zap.String("path", path), zap.Error(err))
			return nil
		}

		mode := info.Mode()
		if mode&0111 == 0 {
			// File is not executable, skip it
			zap.L().Debug("Skipping non-executable file", zap.String("path", path))
			return nil
		}

		// Get tool name from filename (without extension)
		name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))

		// Try to parse shebang to determine interpreter
		// If it fails (e.g., binary executable), interpreter will be empty
		interpreter, err := ParseShebangFromPath(path)

		// Log as an error only if it is a file read error. The incorrect field count error and
		// invalid prefix error are expected for binary executables.
		if err != nil {
			var fileReadErr *ShebangFileReadError
			if errors.As(err, &fileReadErr) {
				zap.L().Error("Failed to read file", zap.Error(err))
			} else {
				zap.L().Debug("Failed to parse shebang (could be a binary executable)", zap.Error(err))
			}
		}

		zap.L().Debug("Parsed interpreter", zap.String("path", path), zap.String("interpreter", interpreter))

		// If a tool with the same name already exists, return an error
		if _, ok := toolMap[name]; ok {
			return NewDuplicateToolNameError(name)
		}

		tool := &ToolEntry{
			Name:        name,
			Path:        path,
			Interpreter: interpreter,
		}

		toolMap[name] = tool
		return nil
	})

	if err != nil {
		return nil, err
	}

	return toolMap, nil
}
