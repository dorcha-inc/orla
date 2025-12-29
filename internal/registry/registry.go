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

// ToolEntry maintains tool information including name, description, repository, maintainer, and keywords.
type ToolEntry struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Repository  string   `yaml:"repository"`
	Maintainer  string   `yaml:"maintainer,omitempty"`
	Keywords    []string `yaml:"keywords,omitempty"`
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

const (
	VersionConstraintLatest = "latest"
	VersionConstraintEmpty  = ""
)

// extractSemverFromTag extracts a semantic version from a git tag.
// Tags must start with 'v' and follow semver format (e.g., "v0.1.0").
// Returns the semantic version string without the 'v' prefix, or empty string if tag doesn't match.
func extractSemverFromTag(tag string) string {
	// Tags must start with 'v'
	if !strings.HasPrefix(tag, "v") {
		return ""
	}
	// Remove 'v' prefix
	version := strings.TrimPrefix(tag, "v")
	// Validate it's a valid semver
	if !semver.IsValid("v" + version) {
		return ""
	}
	return version
}

// ResolveVersion resolves a version constraint to a specific git tag.
// For "latest", it queries git tags from the repository and selects the latest stable version.
// For explicit tags, it returns the tag as-is (validation happens during clone).
func ResolveVersion(tool *ToolEntry, constraint string) (string, error) {
	// Handle explicit tag - user provided it, just return it
	if constraint != VersionConstraintLatest && constraint != VersionConstraintEmpty {
		// Tags must start with 'v'
		if !strings.HasPrefix(constraint, "v") {
			return "", fmt.Errorf("tag '%s' must start with 'v' (e.g., v0.1.0)", constraint)
		}
		return constraint, nil
	}

	// Handle "latest" by querying git tags and finding the latest stable version
	tags, err := defaultGitRunner.ListTags(tool.Repository)
	if err != nil {
		return "", fmt.Errorf("failed to list tags from repository: %w", err)
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found in repository for tool '%s'", tool.Name)
	}

	// Find the latest stable version by parsing semantic versions from tags
	var latestTag string
	var latestVersion string

	for _, tag := range tags {
		semverStr := extractSemverFromTag(tag)
		if semverStr == "" {
			// Skip tags that don't follow semver format
			continue
		}

		// Skip pre-release versions (contain -alpha, -beta, -rc, etc.)
		if !strings.Contains(semverStr, "-") {
			if latestTag == "" || semver.Compare("v"+semverStr, "v"+latestVersion) > 0 {
				latestTag = tag
				latestVersion = semverStr
			}
		}
	}

	if latestTag == "" {
		// If no stable version, use the latest pre-release
		for _, tag := range tags {
			semverStr := extractSemverFromTag(tag)
			if semverStr != "" {
				if latestTag == "" || semver.Compare("v"+semverStr, "v"+latestVersion) > 0 {
					latestTag = tag
					latestVersion = semverStr
				}
			}
		}
	}

	if latestTag == "" {
		return "", fmt.Errorf("no valid semver tags found for tool '%s'. Tags must start with 'v' and follow semver format (e.g., v0.1.0)", tool.Name)
	}

	return latestTag, nil
}

// SearchTools searches the registry for tools matching the query
// It searches in tool names, descriptions, and keywords (case-insensitive)
func SearchTools(registry *RegistryIndex, query string) []ToolEntry {
	// TODO: this is an extremely naive search algorithm. We should use a more sophisticated one
	// with ranking and fuzzy matching.
	if query == "" {
		return registry.Tools
	}

	queryLower := strings.ToLower(query)
	var results []ToolEntry

	for _, tool := range registry.Tools {
		// Check if query matches name
		if strings.Contains(strings.ToLower(tool.Name), queryLower) {
			results = append(results, tool)
			continue
		}

		// Check if query matches description
		if strings.Contains(strings.ToLower(tool.Description), queryLower) {
			results = append(results, tool)
			continue
		}

		// Check if query matches any keyword
		for _, keyword := range tool.Keywords {
			if strings.Contains(strings.ToLower(keyword), queryLower) {
				results = append(results, tool)
				break
			}
		}
	}

	return results
}

// ClearRegistryCache clears the registry cache by removing the entire cache directory
func ClearRegistryCache() error {
	cacheDir, errGetCacheDir := getRegistryCacheDirFunc()
	if errGetCacheDir != nil {
		return fmt.Errorf("failed to get cache directory: %w", errGetCacheDir)
	}

	// Check if cache directory exists
	info, errStat := core.FileStat(cacheDir, "cache directory not found", "failed to stat cache directory")
	if errStat != nil {
		return errStat
	}

	if !info.IsDir() {
		return fmt.Errorf("cache directory is not a directory: %s", cacheDir)
	}

	// Remove the entire cache directory
	errRemove := os.RemoveAll(cacheDir)
	if errRemove != nil {
		return fmt.Errorf("failed to remove cache directory: %w", errRemove)
	}

	fmt.Println("Cache cleared successfully.")
	return nil
}
