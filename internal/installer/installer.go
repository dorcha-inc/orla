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
// toolsDir must be a valid, non-empty directory path
func InstallTool(registryURL, toolName, versionConstraint string, toolsDir string, progressWriter io.Writer) error {
	if toolsDir == "" {
		return fmt.Errorf("tools directory cannot be empty")
	}
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
	// Resolve to absolute path
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve tools directory path: %w", err)
	}

	installDir := filepath.Join(absToolsDir, toolName, manifest.Version)

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

// InstallLocalTool installs a tool from a local directory or archive (archive support not yet implemented)
// toolsDir must be a valid, non-empty directory path
func InstallLocalTool(localPath string, toolsDir string, progressWriter io.Writer) error {
	if toolsDir == "" {
		return fmt.Errorf("tools directory cannot be empty")
	}
	// Resolve local path to absolute path
	absLocalPath, absErr := filepath.Abs(localPath)
	if absErr != nil {
		return fmt.Errorf("failed to resolve local path: %w", absErr)
	}

	// Check if path exists
	info, err := core.FileStat(absLocalPath, "local path does not exist", "failed to stat local path")
	if err != nil {
		return err
	}

	// TODO: Support archives (tarballs, zip files) - for now only directories
	if !info.IsDir() {
		return fmt.Errorf("local path must be a directory (archive support not yet implemented): %s", absLocalPath)
	}

	// Load and validate manifest
	manifest, errLoadManifest := LoadManifest(absLocalPath)
	if errLoadManifest != nil {
		return fmt.Errorf("failed to load manifest: %w", errLoadManifest)
	}

	errValidateManifest := ValidateManifest(manifest, absLocalPath)
	if errValidateManifest != nil {
		return fmt.Errorf("failed to validate manifest: %w", errValidateManifest)
	}

	// Get install directory using version from tool.yaml (source of truth)
	// Resolve to absolute path
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve tools directory path: %w", err)
	}

	installDir := filepath.Join(absToolsDir, manifest.Name, manifest.Version)

	// Install to target directory
	errInstallToDirectory := InstallToDirectory(absLocalPath, installDir, progressWriter)
	if errInstallToDirectory != nil {
		return fmt.Errorf("failed to install tool to directory: %w", errInstallToDirectory)
	}

	zap.L().Info("Local tool installed successfully",
		zap.String("tool", manifest.Name),
		zap.String("version", manifest.Version),
		zap.String("source", absLocalPath),
		zap.String("path", installDir))

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
// toolsDir must be a valid, non-empty directory path
func ListInstalledTools(toolsDir string) ([]InstalledToolInfo, error) {
	if toolsDir == "" {
		return nil, fmt.Errorf("tools directory cannot be empty")
	}

	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tools directory path: %w", err)
	}
	installDir := absToolsDir

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
// toolsDir must be a valid, non-empty directory path
func UninstallTool(toolName string, toolsDir string) error {
	if toolsDir == "" {
		return fmt.Errorf("tools directory cannot be empty")
	}

	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve tools directory path: %w", err)
	}
	installDir := absToolsDir

	toolDir := filepath.Join(installDir, toolName)
	_, errStat := core.FileStat(toolDir, fmt.Sprintf("tool '%s' not installed", toolName), "failed to stat tool directory")
	if errStat != nil {
		return errStat
	}

	// Remove the entire tool directory (includes all versions)
	errRemove := os.RemoveAll(toolDir)
	if errRemove != nil {
		return fmt.Errorf("failed to remove tool directory: %w", err)
	}

	zap.L().Info("Tool uninstalled successfully", zap.String("tool", toolName))
	return nil
}

// UpdateTool updates a tool to the latest version
// toolsDir must be a valid, non-empty directory path
func UpdateTool(registryURL, toolName string, toolsDir string, progressWriter io.Writer) error {
	if toolsDir == "" {
		return fmt.Errorf("tools directory cannot be empty")
	}

	// Check if tool is installed
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve tools directory path: %w", err)
	}
	installDir := absToolsDir

	toolDir := filepath.Join(installDir, toolName)
	_, errStat := core.FileStat(toolDir, fmt.Sprintf("tool '%s' not installed", toolName), "failed to stat tool directory")
	if errStat != nil {
		return errStat
	}

	// Install latest version (InstallTool handles this)
	return InstallTool(registryURL, toolName, registry.VersionConstraintLatest, toolsDir, progressWriter)
}
