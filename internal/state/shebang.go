package state

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ShebangIncorrectFieldCountError is an error that is returned when a shebang line has an incorrect number of fields
type ShebangIncorrectFieldCountError struct {
	Path  string `json:"path"`
	Line  string `json:"line"`
	Count int    `json:"count"`
}

// Error returns the error message for the InvalidShebangError
func (e *ShebangIncorrectFieldCountError) Error() string {
	return fmt.Sprintf("invalid shebang: %s, expected 2 fields, got %d", e.Line, e.Count)
}

// NewShebangIncorrectFieldCountError creates a new ShebangIncorrectFieldCountError
func NewShebangIncorrectFieldCountError(path string, line string, count int) *ShebangIncorrectFieldCountError {
	return &ShebangIncorrectFieldCountError{Path: path, Line: line, Count: count}
}

// Interface guard for ShebangIncorrectFieldCountError
// This ensures that ShebangIncorrectFieldCountError implements the error interface.
var _ error = &ShebangIncorrectFieldCountError{}

type ShebangFileReadError struct {
	Path string `json:"path"`
}

// Error returns the error message for the ShebangIncorrectFileError
func (e *ShebangFileReadError) Error() string {
	return fmt.Sprintf("failed to read shebang file: %s", e.Path)
}

// NewShebangFileReadError creates a new ShebangFileReadError
func NewShebangFileReadError(path string) *ShebangFileReadError {
	return &ShebangFileReadError{Path: path}
}

// Interface guard for ShebangFileReadError
// This ensures that ShebangFileReadError implements the error interface.
var _ error = &ShebangFileReadError{}

type ShebangInvalidPrefixError struct {
	Path string `json:"path"`
	Line string `json:"line"`
}

// Error returns the error message for the ShebangInvalidPrefixError
func (e *ShebangInvalidPrefixError) Error() string {
	return fmt.Sprintf("invalid shebang prefix: %s", e.Line)
}

// NewShebangInvalidPrefixError creates a new ShebangInvalidPrefixError
func NewShebangInvalidPrefixError(path string, line string) *ShebangInvalidPrefixError {
	return &ShebangInvalidPrefixError{Path: path, Line: line}
}

// Interface guard for ShebangInvalidPrefixError
// This ensures that ShebangInvalidPrefixError implements the error interface.
var _ error = &ShebangInvalidPrefixError{}

// ParseShebangFromPath parses the shebang line to determine the interpreter
func ParseShebangFromPath(path string) (string, error) {
	// Open the file
	// #nosec G304 -- path is validated and comes from tool discovery, not direct user input
	file, err := os.Open(path)
	if err != nil {
		return "", NewShebangFileReadError(path)
	}
	defer func() {
		_ = file.Close() // Ignore close errors - file is already read
	}()

	// Read the file
	scanner := bufio.NewScanner(file)
	scanner.Scan()
	line := scanner.Text()

	// Trim the line of any leading/trailing whitespace
	line = strings.TrimSpace(line)

	// Check if line starts with shebang prefix
	if !strings.HasPrefix(line, "#!") {
		return "", NewShebangInvalidPrefixError(path, line)
	}

	// Remove the shebang prefix and trim whitespace
	interpreterLine := strings.TrimSpace(line[2:])

	// Split the remaining line into parts (interpreter and optional arguments)
	parts := strings.Fields(interpreterLine)

	if len(parts) == 0 {
		return "", NewShebangIncorrectFieldCountError(path, line, 0)
	}

	// The first part is the interpreter
	interpreter := parts[0]

	return interpreter, nil
}
