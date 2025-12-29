// Package registry provides functionality for fetching and parsing tool registries.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultRegistryURL is the default registry URL
const DefaultRegistryURL = "https://github.com/dorcha-inc/orla-registry"

// GetOrlaHomeDir returns the ~/.orla directory path
func GetOrlaHomeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get orla home directory: %w", err)
	}
	return filepath.Join(homeDir, ".orla"), nil
}

// GetInstalledToolsDirFunc is a function variable for getting installed tools directory (can be swapped for testing)
var GetInstalledToolsDirFunc = func() (string, error) {
	orlaHome, err := GetOrlaHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get orla home directory: %w", err)
	}
	return filepath.Join(orlaHome, "tools"), nil
}

// GetInstalledToolsDir returns the ~/.orla/tools directory path
func GetInstalledToolsDir() (string, error) {
	return GetInstalledToolsDirFunc()
}

// GetRegistryCacheDir returns the ~/.orla/cache/registry directory path
func GetRegistryCacheDir() (string, error) {
	orlaHome, err := GetOrlaHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get orla home directory: %w", err)
	}
	return filepath.Join(orlaHome, "cache", "registry"), nil
}

// ExtractVersionFromDir extracts the version from a tool directory path
// relative to the install directory. The path structure is expected to be:
// ~/.orla/tools/TOOL-NAME/VERSION/
// Returns the version string and an error if the path structure is invalid.
func ExtractVersionFromDir(toolDir, installDir string) (string, error) {
	relPath, err := filepath.Rel(installDir, toolDir)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid tool directory path structure: expected TOOL-NAME/VERSION/, got %s", relPath)
	}

	return parts[1], nil
}
