// Package registry provides functionality for fetching and parsing tool registries.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agnivade/levenshtein"
	"github.com/dorcha-inc/orla/internal/core"
	"go.uber.org/zap"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// RegistryIndex maintains version information and tool entries for a registry.
type RegistryIndex struct {
	Version     int         `yaml:"version"`
	RegistryURL string      `yaml:"registry_url"`
	Tools       []ToolEntry `yaml:"tools"`
}

// ToolEntry maintains tool information including name, description, repository, versions, maintainer, and keywords.
type ToolEntry struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Repository  string    `yaml:"repository"`
	Versions    []Version `yaml:"versions"`
	Maintainer  string    `yaml:"maintainer,omitempty"`
	Keywords    []string  `yaml:"keywords,omitempty"`
}

// Version maintains version information including version, tag, and checksum.
type Version struct {
	Version  string `yaml:"version"`
	Tag      string `yaml:"tag"`
	Checksum string `yaml:"checksum,omitempty"`
}

// getRegistryCacheDirFunc is a function variable for getting cache directory (can be swapped for testing)
var getRegistryCacheDirFunc = GetRegistryCacheDir

// GetRegistryCacheDirFunc is exported for testing purposes (pointer to function variable)
var GetRegistryCacheDirFunc *func() (string, error) = &getRegistryCacheDirFunc

// FetchRegistry fetches the registry index from the given URL
// If useCache is true, it will use cached registry if available and fresh
func FetchRegistry(registryURL string, useCache bool) (*RegistryIndex, error) {
	cacheDir, err := getRegistryCacheDirFunc()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}

	// Create cache directory if it doesn't exist
	// #nosec G301 -- cache directory permissions 0755 are acceptable for user cache
	if errMkdir := os.MkdirAll(cacheDir, 0755); errMkdir != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", errMkdir)
	}

	// Generate cache key from registry URL
	cacheKey, err := sanitizeURLForCache(registryURL)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}
	cachePath := filepath.Join(cacheDir, cacheKey, "registry.yaml")

	// Check cache if enabled
	if useCache {
		cached, errLoad := loadCachedRegistry(cachePath)
		if errLoad == nil {
			zap.L().Debug("Using cached registry", zap.String("path", cachePath))
			return cached, nil
		}
		zap.L().Debug("Failed to load cached registry, cloning fresh", zap.Error(errLoad), zap.String("path", cachePath))
	}

	// Clone or update registry repository
	registryRepoPath := filepath.Join(cacheDir, cacheKey, "repo")
	registryYAMLPath := filepath.Join(registryRepoPath, "registry.yaml")

	// Check if repo exists, if so update it, otherwise clone it
	if _, errStat := os.Stat(registryRepoPath); errStat == nil {
		// Repository exists, update it
		zap.L().Debug("Updating registry repository", zap.String("path", registryRepoPath))
		if errRun := defaultGitRunner.Pull(registryRepoPath); errRun != nil {
			zap.L().Warn("Failed to update registry repository, cloning fresh", zap.Error(errRun))
			// Remove old repo and clone fresh
			if errRemove := os.RemoveAll(registryRepoPath); errRemove != nil {
				return nil, fmt.Errorf("failed to remove old registry repo: %w", errRemove)
			}
			if errClone := defaultGitRunner.Clone(registryURL, registryRepoPath); errClone != nil {
				return nil, fmt.Errorf("failed to clone registry repository: %w", errClone)
			}
		}
	} else {
		// Repository doesn't exist, clone it
		zap.L().Debug("Cloning registry repository", zap.String("url", registryURL), zap.String("path", registryRepoPath))
		if errClone := defaultGitRunner.Clone(registryURL, registryRepoPath); errClone != nil {
			return nil, fmt.Errorf("failed to clone registry repository: %w", errClone)
		}
	}

	// Read and parse registry.yaml
	// #nosec G304 -- registryYAMLPath is constructed from known cache directory, safe
	data, err := os.ReadFile(registryYAMLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry.yaml: %w", err)
	}

	var index RegistryIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse registry.yaml: %w", err)
	}

	// Update cache
	if useCache {
		if err := saveCachedRegistry(cachePath, &index); err != nil {
			zap.L().Warn("Failed to cache registry", zap.Error(err))
		}
	}

	return &index, nil
}

// cloneRegistry clones the registry repository (deprecated: use defaultGitRunner.Clone instead)
// Kept for backward compatibility with installer package
func cloneRegistry(registryURL, targetPath string) error {
	return defaultGitRunner.Clone(registryURL, targetPath)
}

// loadCachedRegistry loads registry from cache if it's fresh (less than 1 hour old)
func loadCachedRegistry(cachePath string) (*RegistryIndex, error) {
	// Open cache directory as root for secure file access
	cacheDir := filepath.Dir(cachePath)
	root, err := os.OpenRoot(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache directory: %w", err)
	}
	defer core.LogDeferredError(root.Close)

	// Stat file using os.Root (automatically prevents path traversal)
	fileName := filepath.Base(cachePath)
	info, err := root.Stat(fileName)
	if err != nil {
		return nil, err
	}

	// Check if cache is fresh (less than 1 hour old)
	if time.Since(info.ModTime()) > time.Hour {
		return nil, fmt.Errorf("cache expired")
	}

	// Read file using os.Root (automatically prevents path traversal)
	data, err := root.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	var index RegistryIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, err
	}

	return &index, nil
}

// saveCachedRegistry saves registry to cache
func saveCachedRegistry(cachePath string, index *RegistryIndex) error {
	// Open cache directory as root for secure file access
	cacheDir := filepath.Dir(cachePath)
	root, err := os.OpenRoot(cacheDir)
	if err != nil {
		// If directory doesn't exist, create it first
		// #nosec G301 -- cache directory permissions 0755 are acceptable for user cache
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return fmt.Errorf("failed to create cache directory: %w", err)
		}
		root, err = os.OpenRoot(cacheDir)
		if err != nil {
			return fmt.Errorf("failed to open cache directory: %w", err)
		}
	}
	defer core.LogDeferredError(root.Close)

	data, err := yaml.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal registry to yaml: %w", err)
	}

	// Write file using os.Root (automatically prevents path traversal)
	fileName := filepath.Base(cachePath)
	// #nosec G306 -- cache file permissions 0644 are acceptable for user cache files
	return root.WriteFile(fileName, data, 0644)
}

// SanitizeURLForCache converts a URL to a safe cache key using SHA256 hash
// This ensures filesystem-safe cache keys that handle all URL formats correctly
// Exported for testing purposes
func SanitizeURLForCache(rawURL string) (string, error) {
	return sanitizeURLForCache(rawURL)
}

// sanitizeURLForCache converts a URL to a safe cache key using SHA256 hash
// This ensures filesystem-safe cache keys that handle all URL formats correctly
func sanitizeURLForCache(rawURL string) (string, error) {
	// Parse URL to normalize it (handles edge cases)
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Validate that URL has required components (scheme and host)
	if parsedURL.Scheme == "" {
		return "", fmt.Errorf("URL missing scheme: %s", rawURL)
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("URL missing host: %s", rawURL)
	}

	// Normalize URL: remove default ports, ensure consistent scheme format
	normalized := parsedURL.String()

	// Use SHA256 hash for filesystem-safe cache key
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:]), nil
}

// FindTool finds a tool in the registry by name
func FindTool(registry *RegistryIndex, name string) (*ToolEntry, error) {
	for i := range registry.Tools {
		if registry.Tools[i].Name == name {
			return &registry.Tools[i], nil
		}
	}
	return nil, fmt.Errorf("tool '%s' not found in registry", name)
}

// SuggestSimilarToolName finds the most similar tool name for typo detection using Levenshtein distance
func SuggestSimilarToolName(registry *RegistryIndex, name string) string {
	if len(registry.Tools) == 0 {
		return ""
	}

	var bestTool string
	bestDistance := 3 // Only consider distances <= 2

	nameLower := strings.ToLower(name)

	// Find the tool with the smallest Levenshtein distance
	for _, tool := range registry.Tools {
		toolNameLower := strings.ToLower(tool.Name)
		distance := levenshtein.ComputeDistance(nameLower, toolNameLower)
		if distance < bestDistance {
			bestDistance = distance
			bestTool = tool.Name
		}
	}

	if bestTool == "" {
		return ""
	}

	return bestTool
}

// ResolveVersion resolves a version constraint to a specific version
func ResolveVersion(tool *ToolEntry, constraint string) (*Version, error) {
	if len(tool.Versions) == 0 {
		return nil, fmt.Errorf("no versions available for tool '%s'", tool.Name)
	}

	// Handle "latest" constraint
	if constraint == "latest" || constraint == "" {
		// Find the latest stable version (not pre-release)
		var latest *Version
		for i := range tool.Versions {
			v := &tool.Versions[i]
			// Skip pre-release versions (contain -alpha, -beta, -rc, etc.)
			if !strings.Contains(v.Version, "-") {
				if latest == nil || semver.Compare("v"+v.Version, "v"+latest.Version) > 0 {
					latest = v
				}
			}
		}
		if latest == nil {
			// If no stable version, use the latest pre-release
			latest = &tool.Versions[0]
			for i := 1; i < len(tool.Versions); i++ {
				if semver.Compare("v"+tool.Versions[i].Version, "v"+latest.Version) > 0 {
					latest = &tool.Versions[i]
				}
			}
		}
		return latest, nil
	}

	// Handle exact version
	for i := range tool.Versions {
		if tool.Versions[i].Version == constraint {
			return &tool.Versions[i], nil
		}
	}

	return nil, fmt.Errorf("version '%s' not found for tool '%s'", constraint, tool.Name)
}
