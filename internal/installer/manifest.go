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

// ToolManifestFileName is the name of the tool.yaml manifest file as defined in RFC 3
const ToolManifestFileName = "tool.yaml"

// ToolManifest represents an RFC 3 compliant tool.yaml manifest
type ToolManifest struct {
	Name         string         `yaml:"name" validate:"required"`
	Version      string         `yaml:"version" validate:"required"`
	Description  string         `yaml:"description" validate:"required"`
	Entrypoint   string         `yaml:"entrypoint" validate:"required"`
	Author       string         `yaml:"author,omitempty"`
	License      string         `yaml:"license,omitempty"`
	Repository   string         `yaml:"repository,omitempty"`
	Homepage     string         `yaml:"homepage,omitempty"`
	Keywords     []string       `yaml:"keywords,omitempty"`
	Dependencies []string       `yaml:"dependencies,omitempty"`
	Runtime      *RuntimeConfig `yaml:"runtime,omitempty"`
}

// RuntimeMode represents the runtime mode of a tool
type RuntimeMode string

// RuntimeMode constants
const (
	RuntimeModeSimple  RuntimeMode = "simple"
	RuntimeModeCapsule RuntimeMode = "capsule"
)

var validRuntimeModes = []RuntimeMode{RuntimeModeSimple, RuntimeModeCapsule}

// RuntimeConfig represents RFC 3 compliant runtime configuration
type RuntimeConfig struct {
	Mode RuntimeMode `yaml:"mode,omitempty"`
}

// LoadManifest loads and parses a tool.yaml manifest from the given directory
func LoadManifest(toolDir string) (*ToolManifest, error) {
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

	var manifest ToolManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse tool.yaml: %w", err)
	}

	return &manifest, nil
}

var validate = validator.New()

// ValidateManifest validates a tool manifest
func ValidateManifest(manifest *ToolManifest, toolDir string) error {
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
		manifest.Runtime = &RuntimeConfig{
			Mode: RuntimeModeSimple,
		}
	}

	if !slices.Contains(validRuntimeModes, manifest.Runtime.Mode) {
		return fmt.Errorf("invalid runtime.mode: %s", manifest.Runtime.Mode)
	}

	// TODO: implement capsule mode
	if manifest.Runtime.Mode == RuntimeModeCapsule {
		zap.L().Warn("runtime.mode: 'capsule' is not yet implemented, using simple mode", zap.String("tool", manifest.Name))
	}

	return nil
}
