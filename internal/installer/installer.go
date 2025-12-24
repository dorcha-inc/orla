// Package installer provides functionality for installing tool packages.
package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/registry"
)

// InstallTool installs a tool from the registry
func InstallTool(registryURL, toolName, versionConstraint string, progressWriter io.Writer) error {
	// Fetch registry
	reg, errFetchRegistry := registry.FetchRegistry(registryURL, true)
	if errFetchRegistry != nil {
		return fmt.Errorf("failed to fetch registry: %w", errFetchRegistry)
	}

	// Find tool
	tool, errFindTool := registry.FindTool(reg, toolName)
	if errFindTool != nil {
		// Try to suggest similar tool
		suggestion := registry.SuggestSimilarToolName(reg, toolName)
		if suggestion != "" {
			return fmt.Errorf("tool '%s' not found in registry. Did you mean: %s?: %w", toolName, suggestion, errFindTool)
		}
		return fmt.Errorf("tool '%s' not found in registry: %w", toolName, errFindTool)
	}

	// Resolve version constraint to a git tag
	tag, errResolveVersion := registry.ResolveVersion(tool, versionConstraint)
	if errResolveVersion != nil {
		return fmt.Errorf("failed to resolve version: %w", errResolveVersion)
	}

	// Clone tool repository
	tempDir, errCreateTempDir := os.MkdirTemp("", "orla-install-*")
	if errCreateTempDir != nil {
		return fmt.Errorf("failed to create temp directory: %w", errCreateTempDir)
	}
	defer core.LogDeferredError(func() error { return os.RemoveAll(tempDir) })

	cloneDir := filepath.Join(tempDir, "tool")
	if errClone := cloneToolRepository(tool.Repository, tag, cloneDir); errClone != nil {
		return fmt.Errorf("failed to clone tool repository: %w", errClone)
	}

	// Load and validate manifest
	manifest, errLoadManifest := LoadManifest(cloneDir)
	if errLoadManifest != nil {
		return fmt.Errorf("failed to load manifest: %w", errLoadManifest)
	}

	if errValidateManifest := ValidateManifest(manifest, cloneDir); errValidateManifest != nil {
		return fmt.Errorf("failed to validate manifest: %w", errValidateManifest)
	}

	// Validate that git tag matches tool.yaml version
	// Tags must start with 'v' and match the version exactly
	expectedTag := "v" + manifest.Version
	if tag != expectedTag {
		return fmt.Errorf("git tag '%s' does not match tool.yaml version '%s'. Tag must be 'v%s'", tag, manifest.Version, manifest.Version)
	}

	// Get install directory using version from tool.yaml (source of truth)
	installBaseDir, errGetInstallBaseDir := (*registry.GetInstalledToolsDirFunc)()
	if errGetInstallBaseDir != nil {
		return fmt.Errorf("failed to get install base directory: %w", errGetInstallBaseDir)
	}

	installDir := filepath.Join(installBaseDir, toolName, manifest.Version)

	// Install to target directory
	if errInstallToDirectory := InstallToDirectory(cloneDir, installDir, progressWriter); errInstallToDirectory != nil {
		return fmt.Errorf("failed to install tool to directory: %w", errInstallToDirectory)
	}

	zap.L().Info("Tool installed successfully",
		zap.String("tool", toolName),
		zap.String("version", manifest.Version),
		zap.String("tag", tag),
		zap.String("path", installDir))

	return nil
}

// cloneToolRepository clones a tool repository at a specific tag
func cloneToolRepository(repoURL, tag, targetDir string) error {
	zap.L().Debug("Cloning tool repository", zap.String("url", repoURL), zap.String("tag", tag), zap.String("path", targetDir))

	// Clone repository
	cmd := exec.Command("git", "clone", "--depth", "1", "--branch", tag, repoURL, targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If branch doesn't exist, try cloning without branch and checking out tag
		if strings.Contains(string(output), "not found") {
			cmd = exec.Command("git", "clone", "--depth", "1", repoURL, targetDir)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to clone repository: %w", err)
			}
			// Checkout the tag
			cmd = exec.Command("git", "checkout", tag)
			cmd.Dir = targetDir
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to checkout tag %s: %w", tag, err)
			}
		} else {
			return fmt.Errorf("failed to clone repository: %w, output: %s", err, string(output))
		}
	}

	return nil
}

// InstallToDirectory copies tool files from source to target directory
func InstallToDirectory(sourceDir, targetDir string, progressWriter io.Writer) error {
	// Create target directory
	err := os.MkdirAll(targetDir, 0750)
	if err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	// Copy files recursively, skipping .git directory
	return core.CopyDirectory(sourceDir, targetDir, []string{".git"})
}

// InstalledToolInfo represents information about an installed tool
type InstalledToolInfo struct {
	Name        string
	Version     string
	Path        string
	Description string
}

// ListInstalledTools returns a list of all installed tools with their versions
// Multiple versions of the same tool will be listed separately
func ListInstalledTools() ([]InstalledToolInfo, error) {
	installDir, err := (*registry.GetInstalledToolsDirFunc)()
	if err != nil {
		return nil, fmt.Errorf("failed to get install directory: %w", err)
	}

	var tools []InstalledToolInfo

	// Walk through installed tools directory
	err = filepath.WalkDir(installDir, func(path string, d os.DirEntry, errWalk error) error {
		if errWalk != nil {
			return fmt.Errorf("failed to walk installed tools directory: %w", errWalk)
		}

		// Look for tool.yaml files
		if d.Name() == ToolManifestFileName {
			if d.IsDir() {
				// we should not have a directory with the tool manifest file name
				return fmt.Errorf("%s is a directory, not a file, creating a directory with the same name as the tool manifest file is not allowed", path)
			}

			// Get the tool directory that contains the tool manifest file
			toolDir := filepath.Dir(path)

			// Load manifest
			manifest, errManifest := LoadManifest(toolDir)
			if errManifest != nil {
				zap.L().Debug("Failed to load manifest, skipping", zap.String("path", path), zap.Error(errManifest))
				return nil
			}

			// Extract version from path: ~/.orla/tools/TOOL-NAME/VERSION/
			version, errVersion := registry.ExtractVersionFromDir(toolDir, installDir)
			if errVersion != nil {
				zap.L().Debug("Failed to extract version from path, skipping", zap.String("path", toolDir), zap.Error(errVersion))
				return nil
			}

			newTool := InstalledToolInfo{
				Name:        manifest.Name,
				Version:     version,
				Path:        toolDir,
				Description: manifest.Description,
			}

			tools = append(tools, newTool)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk installed tools directory: %w", err)
	}

	return tools, nil
}

// UninstallTool removes an installed tool
func UninstallTool(toolName string) error {
	installDir, err := (*registry.GetInstalledToolsDirFunc)()
	if err != nil {
		return fmt.Errorf("failed to get install directory: %w", err)
	}

	toolDir := filepath.Join(installDir, toolName)
	if _, err := os.Stat(toolDir); os.IsNotExist(err) {
		return fmt.Errorf("tool '%s' is not installed", toolName)
	}

	// Remove the entire tool directory (includes all versions)
	if err := os.RemoveAll(toolDir); err != nil {
		return fmt.Errorf("failed to remove tool directory: %w", err)
	}

	zap.L().Info("Tool uninstalled successfully", zap.String("tool", toolName))
	return nil
}

// UpdateTool updates a tool to the latest version
func UpdateTool(registryURL, toolName string, progressWriter io.Writer) error {
	// Check if tool is installed
	installDir, errInstallDir := (*registry.GetInstalledToolsDirFunc)()
	if errInstallDir != nil {
		return fmt.Errorf("failed to get install directory: %w", errInstallDir)
	}

	toolDir := filepath.Join(installDir, toolName)
	if _, errStat := os.Stat(toolDir); errStat != nil {
		if os.IsNotExist(errStat) {
			return fmt.Errorf("tool '%s' is not installed: %w", toolName, errStat)
		}
		return fmt.Errorf("failed to stat tool directory '%s': %w", toolDir, errStat)
	}

	// Install latest version (InstallTool handles this)
	return InstallTool(registryURL, toolName, registry.VersionConstraintLatest, progressWriter)
}
