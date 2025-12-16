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
	reg, err := registry.FetchRegistry(registryURL, true)
	if err != nil {
		return fmt.Errorf("failed to fetch registry: %w", err)
	}

	// Find tool
	tool, err := registry.FindTool(reg, toolName)
	if err != nil {
		// Try to suggest similar tool
		suggestion := registry.SuggestSimilarToolName(reg, toolName)
		if suggestion != "" {
			return fmt.Errorf("tool '%s' not found in registry. Did you mean: %s?", toolName, suggestion)
		}
		return err
	}

	// Resolve version
	version, err := registry.ResolveVersion(tool, versionConstraint)
	if err != nil {
		return err
	}

	// Get install directory
	installBaseDir, err := registry.GetInstalledToolsDir()
	if err != nil {
		return fmt.Errorf("failed to get install directory: %w", err)
	}

	installDir := filepath.Join(installBaseDir, toolName, version.Version)

	// Clone tool repository
	tempDir, err := os.MkdirTemp("", "orla-install-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer core.LogDeferredError(func() error { return os.RemoveAll(tempDir) })

	cloneDir := filepath.Join(tempDir, "tool")
	if errClone := cloneToolRepository(tool.Repository, version.Tag, cloneDir); errClone != nil {
		return errClone
	}

	// Load and validate manifest
	manifest, err := LoadManifest(cloneDir)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	if err := ValidateManifest(manifest, cloneDir); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	// Install to target directory
	if err := InstallToDirectory(cloneDir, installDir, progressWriter); err != nil {
		return fmt.Errorf("failed to install tool: %w", err)
	}

	zap.L().Info("Tool installed successfully",
		zap.String("tool", toolName),
		zap.String("version", version.Version),
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
	if err := os.MkdirAll(targetDir, 0750); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	// Copy files recursively, skipping .git directory
	return core.CopyDirectory(sourceDir, targetDir, []string{".git"})
}
