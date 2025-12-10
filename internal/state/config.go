// Package state provides all the functionality for managing the state of the Orla server.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// OrlaConfig represents the orla configuration, including
// the tools directory, the port to listen on, the timeout
// for tool executions, the log format, and the log level.
type OrlaConfig struct {
	ToolsDir      string         `json:"tools_dir,omitempty"`      // the directory containing the tools
	ToolsRegistry *ToolsRegistry `json:"tools_registry,omitempty"` // the tools registry
	Port          int            `json:"port,omitempty"`           // the port to listen on
	Timeout       int            `json:"timeout,omitempty"`        // the timeout for tool executions in seconds
	LogFormat     string         `json:"log_format,omitempty"`     // the log format, "json" or "pretty"
	LogLevel      string         `json:"log_level,omitempty"`      // the log level, "debug", "info", "warn", "error", "fatal"
}

// NewDefaultOrlaConfig returns a configuration with default values
func NewDefaultOrlaConfig() (*OrlaConfig, error) {
	toolsDir := "./tools"

	// Resolve relative path to absolute path (relative to current working directory)
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tools directory path: %w", err)
	}

	toolsRegistry, err := NewToolsRegistryFromDirectory(absToolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create tools registry: %w", err)
	}

	return &OrlaConfig{
		ToolsDir:      absToolsDir,
		ToolsRegistry: toolsRegistry,
		Port:          8080,
		Timeout:       30,
		LogFormat:     "json",
		LogLevel:      "info",
	}, nil
}

// NewOrlaConfigFromPath loads configuration from a JSON file, or returns defaults if no file is provided
func NewOrlaConfigFromPath(path string) (*OrlaConfig, error) {
	// #nosec G304 -- path is provided by user configuration, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg OrlaConfig
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// If the tools dir is unset, the user has to define a tools registry.
	if cfg.ToolsDir == "" {
		if cfg.ToolsRegistry == nil {
			return nil, fmt.Errorf("tools dir is unset and tools registry is not defined in the config file: %s", path)
		}

		// If the tools registry is defined, we can use it to scan the tools directory.
		zap.L().Debug("Using tools registry entry in config file directly", zap.String("path", path))
		return &cfg, nil
	}

	// Resolve relative paths
	if !filepath.IsAbs(cfg.ToolsDir) {
		configDir := filepath.Dir(path)
		cfg.ToolsDir = filepath.Join(configDir, cfg.ToolsDir)
		zap.L().Debug("Resolved tools dir to absolute path", zap.String("path", cfg.ToolsDir))
	}

	// Reload tools registry from the configured directory
	toolsRegistry, err := NewToolsRegistryFromDirectory(cfg.ToolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create tools registry: %w", err)
	}
	cfg.ToolsRegistry = toolsRegistry

	return &cfg, nil
}
