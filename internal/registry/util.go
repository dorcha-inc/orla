// Package registry provides functionality for fetching and parsing tool registries.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
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

// GetInstalledToolsDir returns the ~/.orla/tools directory path
func GetInstalledToolsDir() (string, error) {
	orlaHome, err := GetOrlaHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get orla home directory: %w", err)
	}
	return filepath.Join(orlaHome, "tools"), nil
}

// GetRegistryCacheDir returns the ~/.orla/cache/registry directory path
func GetRegistryCacheDir() (string, error) {
	orlaHome, err := GetOrlaHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get orla home directory: %w", err)
	}
	return filepath.Join(orlaHome, "cache", "registry"), nil
}
