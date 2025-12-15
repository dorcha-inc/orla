// Package state provides all the functionality for managing the state of the Orla server.
package state

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// OrlaConfig represents the orla configuration, including
// the tools directory, the port to listen on, the timeout
// for tool executions, the log format, and the log level.
type OrlaConfig struct {
	ToolsDir      string         `yaml:"tools_dir,omitempty"`      // the directory containing the tools
	ToolsRegistry *ToolsRegistry `yaml:"tools_registry,omitempty"` // the tools registry
	Port          int            `yaml:"port,omitempty"`           // the port to listen on
	Timeout       int            `yaml:"timeout,omitempty"`        // the timeout for tool executions in seconds
	LogFormat     string         `yaml:"log_format,omitempty"`     // the log format, "json" or "pretty"
	LogLevel      string         `yaml:"log_level,omitempty"`      // the log level, "debug", "info", "warn", "error", "fatal"
}

// NewDefaultOrlaConfig returns a configuration with default values
func NewDefaultOrlaConfig() (*OrlaConfig, error) {
	cfg := &OrlaConfig{
		Port:      8080,
		Timeout:   30,
		LogFormat: "json",
		LogLevel:  "info",
	}

	toolsDir := "./tools"
	if err := cfg.SetToolsDir(toolsDir); err != nil {
		return nil, fmt.Errorf("failed to set tools directory: %w", err)
	}

	// Validate configuration values
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid default configuration: %w", err)
	}

	return cfg, nil
}

// NewOrlaConfigFromPath loads configuration from a YAML file, or returns defaults if no file is provided
func NewOrlaConfigFromPath(path string) (*OrlaConfig, error) {
	// #nosec G304 -- path is provided by user configuration, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg OrlaConfig
	if err = yaml.Unmarshal(data, &cfg); err != nil {
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

	// Resolve relative paths relative to the config file directory before calling SetToolsDir
	toolsDir := cfg.ToolsDir
	if !filepath.IsAbs(toolsDir) {
		configDir := filepath.Dir(path)
		toolsDir = filepath.Join(configDir, toolsDir)
		toolsDir = filepath.Clean(toolsDir)
	}

	// Set tools directory (SetToolsDir will resolve to absolute path)
	if err := cfg.SetToolsDir(toolsDir); err != nil {
		return nil, fmt.Errorf("failed to set tools directory from config file: %w", err)
	}

	// Validate configuration values
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// SetToolsDir updates the tools directory and rebuilds the tools registry.
// The toolsDir parameter can be relative or absolute.
// If relative, it will be resolved to an absolute path relative to the current working directory.
func (cfg *OrlaConfig) SetToolsDir(toolsDir string) error {
	// Validate tools directory
	if toolsDir == "" {
		return fmt.Errorf("tools directory cannot be empty")
	}

	// Resolve relative path to absolute path
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return fmt.Errorf("failed to resolve tools directory path: %w", err)
	}

	cfg.ToolsDir = absToolsDir
	// Rebuild tools registry with the new directory
	toolsRegistry, err := NewToolsRegistryFromDirectory(absToolsDir)
	if err != nil {
		return fmt.Errorf("failed to create tools registry: %w", err)
	}
	cfg.ToolsRegistry = toolsRegistry
	return nil
}

// validateConfig validates configuration values and sets defaults
func validateConfig(cfg *OrlaConfig) error {
	// Set default timeout if not specified
	if cfg.Timeout == 0 {
		cfg.Timeout = 30
	}

	// Validate port
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535, got %d", cfg.Port)
	}

	// Validate timeout
	if cfg.Timeout < 1 {
		return fmt.Errorf("timeout must be at least 1 second, got %d", cfg.Timeout)
	}

	// Warn if timeout is very large (3600 seconds = 1 hour)
	if cfg.Timeout > 3600 {
		zap.L().Warn("Timeout is very large, consider using a value less than 3600 seconds",
			zap.Int("timeout", cfg.Timeout))
	}

	// Validate log format
	if cfg.LogFormat != "" && cfg.LogFormat != "json" && cfg.LogFormat != "pretty" {
		return fmt.Errorf("log_format must be 'json' or 'pretty', got '%s'", cfg.LogFormat)
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
		"fatal": true,
	}
	if cfg.LogLevel != "" && !validLogLevels[cfg.LogLevel] {
		return fmt.Errorf("log_level must be one of: debug, info, warn, error, fatal, got '%s'", cfg.LogLevel)
	}

	return nil
}
