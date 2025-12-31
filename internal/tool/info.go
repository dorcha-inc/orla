package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
	"go.uber.org/zap"
	"golang.org/x/mod/semver"
)

// InfoOptions configures the output format for tool info
type InfoOptions struct {
	JSON   bool
	Writer io.Writer
}

// GetToolInfo retrieves and displays detailed information about an installed tool
func GetToolInfo(toolName string, opts InfoOptions) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	// Load config to get ToolsDir (handles project > user > default precedence)
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.ToolsDir == "" {
		return fmt.Errorf("tools directory not configured")
	}
	installDir := cfg.ToolsDir

	// Find the tool directory (use latest version if multiple exist)
	toolBaseDir := filepath.Join(installDir, toolName)
	if _, err := os.Stat(toolBaseDir); os.IsNotExist(err) {
		return fmt.Errorf("tool '%s' is not installed", toolName)
	}

	// Find the latest version
	toolDir, version, err := findLatestToolVersion(toolBaseDir)
	if err != nil {
		return fmt.Errorf("failed to find tool version: %w", err)
	}

	// Load manifest
	manifest, err := installer.LoadManifest(toolDir)
	if err != nil {
		return fmt.Errorf("failed to load tool manifest: %w", err)
	}

	if opts.JSON {
		// Set Path and Version from runtime values (directory is source of truth for version)
		manifest.Path = toolDir
		manifest.Version = version
		encoder := json.NewEncoder(opts.Writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(manifest)
	}

	// Human-readable output
	core.MustFprintf(opts.Writer, "Name:        %s\n", manifest.Name)
	core.MustFprintf(opts.Writer, "Version:     %s\n", version)
	core.MustFprintf(opts.Writer, "Description: %s\n", manifest.Description)

	if manifest.Author != "" {
		core.MustFprintf(opts.Writer, "Author:      %s\n", manifest.Author)
	}

	if manifest.License != "" {
		core.MustFprintf(opts.Writer, "License:     %s\n", manifest.License)
	}

	if manifest.Repository != "" {
		core.MustFprintf(opts.Writer, "Repository:  %s\n", manifest.Repository)
	}

	if manifest.Homepage != "" {
		core.MustFprintf(opts.Writer, "Homepage:    %s\n", manifest.Homepage)
	}

	core.MustFprintf(opts.Writer, "Entrypoint:  %s\n", manifest.Entrypoint)
	core.MustFprintf(opts.Writer, "Path:        %s\n", toolDir)

	if len(manifest.Keywords) > 0 {
		core.MustFprintf(opts.Writer, "Keywords:    %v\n", manifest.Keywords)
	}

	if len(manifest.Dependencies) > 0 {
		core.MustFprintf(opts.Writer, "Dependencies:\n")
		for _, dep := range manifest.Dependencies {
			core.MustFprintf(opts.Writer, "  - %s\n", dep)
		}
	}

	// Note: Permissions field may not exist in current ToolManifest struct
	// This is a placeholder for future RFC 3 permissions support

	if manifest.Runtime != nil {
		core.MustFprintf(opts.Writer, "Runtime Mode: %s\n", manifest.Runtime.Mode)
		if len(manifest.Runtime.Env) > 0 {
			core.MustFprintf(opts.Writer, "Environment Variables:\n")
			for k, v := range manifest.Runtime.Env {
				core.MustFprintf(opts.Writer, "  %s=%s\n", k, v)
			}
		}
		if len(manifest.Runtime.Args) > 0 {
			core.MustFprintf(opts.Writer, "Arguments: %v\n", manifest.Runtime.Args)
		}
	}

	return nil
}

// findLatestToolVersion finds the latest version of a tool in the tool base directory
func findLatestToolVersion(toolBaseDir string) (toolDir string, version string, err error) {
	entries, err := os.ReadDir(toolBaseDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to read tool base directory: %w", err)
	}

	var latestVersion string
	var latestPath string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		versionDir := filepath.Join(toolBaseDir, entry.Name())
		manifestPath := filepath.Join(versionDir, installer.ToolManifestFileName)

		// Check if this directory contains a valid tool
		_, err := os.Stat(manifestPath)
		if err != nil {
			zap.L().Warn("Failed to stat manifest file, skipping", zap.String("path", manifestPath), zap.Error(err))
			continue
		}

		versionStr := entry.Name()
		// Compare using semantic versioning
		if latestVersion == "" || semver.Compare("v"+versionStr, "v"+latestVersion) > 0 {
			latestVersion = versionStr
			latestPath = versionDir
		}
	}

	if latestPath == "" {
		return "", "", fmt.Errorf("no valid tool versions found in %s", toolBaseDir)
	}

	return latestPath, latestVersion, nil
}
