package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/mod/semver"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
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

// ScanToolsFromDirectory scans the tools directory for executable files using os.Root for secure access
func ScanToolsFromDirectory(dir string) (map[string]*core.ToolEntry, error) {
	toolMap := make(map[string]*core.ToolEntry)

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

	// Open directory as root for secure file access
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open tools directory: %w", err)
	}
	defer core.LogDeferredError(root.Close)

	// Use fs.WalkDir with os.Root for secure directory traversal
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Check if file is executable using os.Root
		info, err := root.Stat(path)
		if err != nil {
			zap.L().Warn("Failed to get file info, skipping", zap.String("path", path), zap.Error(err))
			return nil
		}

		if !core.IsExecutable(info) {
			// File is not executable, skip it
			zap.L().Debug("Skipping non-executable file", zap.String("path", path))
			return nil
		}

		// Get tool name from filename (without extension)
		name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))

		// Try to parse shebang to determine interpreter using os.Root
		// If it fails (e.g., binary executable), interpreter will be empty
		interpreter, err := ParseShebangFromRoot(root, path)

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

		// Resolve absolute path for tool entry
		absPath := filepath.Join(dir, path)

		tool := &core.ToolEntry{
			Name:        name,
			Path:        absPath,
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

// ScanInstalledTools scans ~/.orla/tools/ for installed tools with tool.yaml manifests
func ScanInstalledTools(installDir string) (map[string]*core.ToolEntry, error) {
	toolMap := make(map[string]*core.ToolEntry)

	// Check if directory exists
	info, err := os.Stat(installDir)
	if err != nil {
		if os.IsNotExist(err) {
			zap.L().Debug("Installed tools directory does not exist", zap.String("directory", installDir))
			return toolMap, nil // Return empty map, not an error
		}
		return nil, fmt.Errorf("failed to access installed tools directory %s: %w", installDir, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("installed tools directory path is not a directory: %s", installDir)
	}

	// Scan for tool directories: ~/.orla/tools/TOOL-NAME/VERSION/
	err = filepath.WalkDir(installDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Look for tool.yaml files
		if d.Name() == "tool.yaml" && !d.IsDir() {
			toolDir := filepath.Dir(path)

			// Load and validate manifest
			manifest, err := installer.LoadManifest(toolDir)
			if err != nil {
				zap.L().Warn("Failed to load manifest, skipping", zap.String("path", path), zap.Error(err))
				return nil // Skip invalid manifests
			}

			// Validate manifest
			if errValidate := installer.ValidateManifest(manifest, toolDir); errValidate != nil {
				zap.L().Warn("Manifest validation failed, skipping", zap.String("path", path), zap.Error(errValidate))
				return nil // Skip invalid manifests
			}

			// Open tool directory as root for secure file access
			toolRoot, errOpen := os.OpenRoot(toolDir)
			if errOpen != nil {
				zap.L().Warn("Failed to open tool directory, skipping", zap.String("path", toolDir), zap.Error(errOpen))
				return nil
			}
			defer core.LogDeferredError(toolRoot.Close)

			// Parse interpreter from entrypoint using os.Root (automatically prevents path traversal)
			interpreter, errParse := ParseShebangFromRoot(toolRoot, manifest.Entrypoint)
			if errParse != nil {
				// Not an error for binary executables
				zap.L().Debug("Failed to parse shebang (could be a binary executable)", zap.String("path", manifest.Entrypoint), zap.Error(errParse))
			}

			// Resolve absolute entrypoint path for tool entry
			entrypointPath := filepath.Join(toolDir, manifest.Entrypoint)
			absEntrypoint, errResolve := filepath.Abs(entrypointPath)
			if errResolve != nil {
				zap.L().Warn("Failed to resolve entrypoint path, skipping", zap.String("path", entrypointPath), zap.Error(errResolve))
				return nil
			}

			// Check for duplicate tool names
			if _, ok := toolMap[manifest.Name]; ok {
				// If multiple versions exist, prefer the latest one
				existingTool := toolMap[manifest.Name]

				// Compare versions - if new version is newer, replace
				existingVersion, errVersion := getVersionFromPath(existingTool.Path, installDir)
				if errVersion != nil {
					zap.L().Warn("Failed to extract version from path, skipping version comparison", zap.String("path", existingTool.Path), zap.Error(errVersion))
					// Keep existing tool if version extraction fails
					return nil
				}
				if existingVersion != "" && semver.Compare("v"+manifest.Version, "v"+existingVersion) > 0 {
					zap.L().Debug("Found newer version of tool, using it", zap.String("tool", manifest.Name), zap.String("version", manifest.Version))
				} else {
					zap.L().Debug("Found older version of tool, keeping existing", zap.String("tool", manifest.Name), zap.String("version", manifest.Version))
					return nil // Keep existing version
				}
			}

			// Extract input schema from manifest if present
			var inputSchema map[string]any
			if manifest.MCP != nil && manifest.MCP.InputSchema != nil {
				inputSchema = manifest.MCP.InputSchema
			}

			tool := &core.ToolEntry{
				Name:        manifest.Name,
				Description: manifest.Description,
				Path:        absEntrypoint,
				Interpreter: interpreter,
				InputSchema: inputSchema,
			}

			toolMap[manifest.Name] = tool
			zap.L().Debug("Loaded installed tool", zap.String("tool", manifest.Name), zap.String("version", manifest.Version), zap.String("path", toolDir))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return toolMap, nil
}

// getVersionFromPath extracts version from path like ~/.orla/tools/TOOL-NAME/VERSION/
// Returns an error if toolPath is not within installDir (security check via os.Root)
// toolPath is expected to be an absolute path
func getVersionFromPath(toolPath, installDir string) (string, error) {
	// Resolve installDir to absolute path (required for os.OpenRoot)
	absInstallDir, err := filepath.Abs(installDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve install directory: %w", err)
	}

	// Open root - os.Root will automatically reject paths that escape the root
	root, err := os.OpenRoot(absInstallDir)
	if err != nil {
		return "", fmt.Errorf("failed to open install directory: %w", err)
	}
	defer core.LogDeferredError(root.Close)

	// Compute relative path (toolPath is already absolute)
	relPath, err := filepath.Rel(absInstallDir, toolPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	// Use os.Root.Stat to validate - this will fail if path escapes root
	_, err = root.Stat(relPath)
	if err != nil {
		return "", fmt.Errorf("tool path %s is not within install directory %s: %w", toolPath, installDir, err)
	}

	// Path structure: TOOL-NAME/VERSION/entrypoint
	parts := strings.Split(relPath, string(filepath.Separator))

	if len(parts) < 2 {
		return "", fmt.Errorf("tool path %s is not within install directory %s", toolPath, installDir)
	}

	version := parts[1]

	// try to parse version as semver
	if !semver.IsValid("v" + version) {
		return "", fmt.Errorf("invalid version: %s", version)
	}

	return version, nil
}
