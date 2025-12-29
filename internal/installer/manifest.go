// Package installer provides functionality for installing tool packages.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/dorcha-inc/orla/internal/core"
)

const (
	// DefaultStartupTimeoutMs is the default maximum time Orla will wait for the startup handshake in milliseconds
	DefaultStartupTimeoutMs = 5000
	// DefaultHotLoadDebounceMs is the default minimum debounce interval for file change events in milliseconds
	DefaultHotLoadDebounceMs = 100
)

// ToolManifestFileName is the name of the tool.yaml manifest file as defined in RFC 3
const ToolManifestFileName = "tool.yaml"

var validRuntimeModes = []core.RuntimeMode{core.RuntimeModeSimple, core.RuntimeModeCapsule}
var validHotLoadModes = []core.HotLoadMode{core.HotLoadModeRestart}

// LoadManifest loads and parses a tool.yaml manifest from the given directory
func LoadManifest(toolDir string) (*core.ToolManifest, error) {
	// Open root directory for secure file access
	root, err := os.OpenRoot(toolDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open tool directory: %w", err)
	}
	defer core.LogDeferredError(root.Close)

	// Read manifest file using os.Root (prevents path traversal)
	data, err := root.ReadFile(ToolManifestFileName)
	if err != nil {
		return nil, fmt.Errorf("failed to read tool.yaml: %w", err)
	}

	var manifest core.ToolManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse tool.yaml: %w", err)
	}

	return &manifest, nil
}

var validate = validator.New()

// ValidateManifest validates a tool manifest
func ValidateManifest(manifest *core.ToolManifest, toolDir string) error {
	// Validate required fields using struct tags
	if err := validate.Struct(manifest); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	// Validate entrypoint exists and is within tool directory
	// os.Root automatically prevents path traversal, so we can use it directly
	root, err := os.OpenRoot(toolDir)
	if err != nil {
		return fmt.Errorf("failed to open tool directory: %w", err)
	}
	defer core.LogDeferredError(root.Close)

	// Stat entrypoint using os.Root (automatically prevents path traversal and normalizes paths)
	info, err := root.Stat(manifest.Entrypoint)
	if err != nil {
		return fmt.Errorf("failed to validate entrypoint: %w", err)
	}

	// Check that entrypoint is executable or has an interpreter specified
	if !core.IsExecutable(info) {
		// File is not executable, this is okay if it's a script with shebang
		// or if runtime.interpreter is specified (future feature)
		entrypointPath := filepath.Join(toolDir, manifest.Entrypoint)
		zap.L().Debug("Entrypoint is not executable, assuming script with interpreter", zap.String("path", entrypointPath))
	}

	if manifest.Runtime == nil || manifest.Runtime.Mode == "" {
		manifest.Runtime = &core.RuntimeConfig{
			Mode: core.RuntimeModeSimple,
		}
	}

	if !slices.Contains(validRuntimeModes, manifest.Runtime.Mode) {
		return fmt.Errorf("invalid runtime.mode: %s", manifest.Runtime.Mode)
	}

	// Set default startup timeout for capsule mode
	if manifest.Runtime.Mode == core.RuntimeModeCapsule && manifest.Runtime.StartupTimeoutMs == 0 {
		manifest.Runtime.StartupTimeoutMs = DefaultStartupTimeoutMs
	}

	// Validate hot_load configuration
	if manifest.Runtime.HotLoad != nil {
		if manifest.Runtime.HotLoad.Mode == "" {
			manifest.Runtime.HotLoad.Mode = core.HotLoadModeRestart
		}

		if !slices.Contains(validHotLoadModes, manifest.Runtime.HotLoad.Mode) {
			return fmt.Errorf("invalid runtime.hot_load.mode: %s. ", manifest.Runtime.HotLoad.Mode)
		}

		// Set default debounce if not specified
		if manifest.Runtime.HotLoad.DebounceMs == 0 {
			manifest.Runtime.HotLoad.DebounceMs = DefaultHotLoadDebounceMs
		}
	}

	return nil
}
