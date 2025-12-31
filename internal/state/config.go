// Package state provides all the functionality for managing the state of the Orla server.
package state

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type OrlaOutputFormat string

const (
	OrlaOutputFormatAuto  OrlaOutputFormat = "auto"
	OrlaOutputFormatRich  OrlaOutputFormat = "rich"
	OrlaOutputFormatPlain OrlaOutputFormat = "plain"
)

// OrlaConfig represents the orla configuration, including
// the tools directory, the port to listen on, the timeout
// for tool executions, the log format, and the log level.
// It also includes Agent Mode configuration (RFC 4).
type OrlaConfig struct {
	// Server mode configuration (RFC 1)
	ToolsDir      string         `yaml:"tools_dir,omitempty"`      // the directory containing the tools
	ToolsRegistry *ToolsRegistry `yaml:"tools_registry,omitempty"` // the tools registry
	Port          int            `yaml:"port,omitempty"`           // the port to listen on
	Timeout       int            `yaml:"timeout,omitempty"`        // the timeout for tool executions in seconds
	LogFormat     string         `yaml:"log_format,omitempty"`     // the log format, "json" or "pretty"
	LogLevel      string         `yaml:"log_level,omitempty"`      // the log level, "debug", "info", "warn", "error", "fatal"
	LogFile       string         `yaml:"log_file,omitempty"`       // optional log file path

	// Agent mode configuration (RFC 4)
	Model                      string           `yaml:"model,omitempty"`                         // model identifier (e.g., "ollama:ministral-3:8b", "openai:gpt-4")
	AutoStartOllama            bool             `yaml:"auto_start_ollama,omitempty"`             // automatically start Ollama if not running
	AutoConfigureOllamaService bool             `yaml:"auto_configure_ollama_service,omitempty"` // configure Ollama as system service
	MaxToolCalls               int              `yaml:"max_tool_calls,omitempty"`                // maximum tool calls per prompt
	Streaming                  bool             `yaml:"streaming,omitempty"`                     // enable streaming responses
	OutputFormat               OrlaOutputFormat `yaml:"output_format,omitempty"`                 // output format: "auto", "rich", or "plain"
	ConfirmDestructive         bool             `yaml:"confirm_destructive,omitempty"`           // prompt for destructive actions
	DryRun                     bool             `yaml:"dry_run,omitempty"`                       // default to non-dry-run mode
}

// NewDefaultOrlaConfig returns a configuration with default values
func NewDefaultOrlaConfig() (*OrlaConfig, error) {
	cfg := &OrlaConfig{
		// Server mode defaults (RFC 1)
		Port:      8080,
		Timeout:   30,
		LogFormat: "json",
		LogLevel:  "info",
		LogFile:   "", // No log file by default

		// Agent mode defaults (RFC 4)
		// Default model: ministral-3:8b - designed for edge deployment, works well on laptops
		// Supports tool calling and is optimized for agentic workflows
		Model:                      "ollama:ministral-3:8b",
		AutoStartOllama:            true,
		AutoConfigureOllamaService: false, // Don't auto-configure service by default (requires user consent)
		MaxToolCalls:               10,
		Streaming:                  true,
		OutputFormat:               OrlaOutputFormatAuto, // Auto-detect TTY/colors
		ConfirmDestructive:         true,
		DryRun:                     false,
	}

	toolsDir := "./tools"
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tools directory path: %w", err)
	}
	cfg.ToolsDir = absToolsDir
	if err := cfg.rebuildToolsRegistry(); err != nil {
		return nil, fmt.Errorf("failed to create tools registry: %w", err)
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

		// Resolve tool paths relative to the config file directory
		configDir := filepath.Dir(path)
		for _, tool := range cfg.ToolsRegistry.Tools {
			// Resolve Path if it's relative
			if tool.Path != "" && !filepath.IsAbs(tool.Path) {
				tool.Path = filepath.Join(configDir, tool.Path)
				tool.Path = filepath.Clean(tool.Path)
				absPath, absErr := filepath.Abs(tool.Path)
				if absErr != nil {
					return nil, fmt.Errorf("failed to resolve tool path: %w", absErr)
				}
				tool.Path = absPath
			}

			// Resolve entrypoint to Path if Path is not set
			if tool.Path == "" && tool.Entrypoint != "" {
				entrypointPath := filepath.Join(configDir, tool.Entrypoint)
				entrypointPath = filepath.Clean(entrypointPath)
				absPath, absErr := filepath.Abs(entrypointPath)
				if absErr != nil {
					return nil, fmt.Errorf("failed to resolve entrypoint path: %w", absErr)
				}
				tool.Path = absPath
			}
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

	// Set tools directory (resolve to absolute path)
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tools directory path: %w", err)
	}
	cfg.ToolsDir = absToolsDir
	if err := cfg.rebuildToolsRegistry(); err != nil {
		return nil, fmt.Errorf("failed to create tools registry: %w", err)
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
	// Rebuild tools registry with the new directory and merge with installed tools
	if err := cfg.rebuildToolsRegistry(); err != nil {
		return fmt.Errorf("failed to create tools registry: %w", err)
	}
	return nil
}

// rebuildToolsRegistry rebuilds the tools registry from both directory scan and installed tools
func (cfg *OrlaConfig) rebuildToolsRegistry() error {
	// Start with directory-scanned tools
	dirTools, err := ScanToolsFromDirectory(cfg.ToolsDir)
	if err != nil {
		return err
	}

	// Get installed tools directory
	installDir, err := getInstalledToolsDir()
	if err != nil {
		zap.L().Debug("Failed to get installed tools directory, skipping installed tools", zap.Error(err))
	} else {
		// Scan installed tools
		installedTools, err := ScanInstalledTools(installDir)
		if err != nil {
			zap.L().Warn("Failed to scan installed tools", zap.Error(err))
		} else {
			// Merge installed tools with directory tools
			// Installed tools take precedence if there's a name conflict
			for name, tool := range installedTools {
				if _, exists := dirTools[name]; exists {
					zap.L().Debug("Tool found in both directory and installed tools, using installed version", zap.String("tool", name))
				}
				dirTools[name] = tool
			}
		}
	}

	cfg.ToolsRegistry = &ToolsRegistry{Tools: dirTools}
	return nil
}

// getInstalledToolsDir returns the installed tools directory path
func getInstalledToolsDir() (string, error) {
	// Import registry package to use its helper
	// We can't import registry here due to circular dependency risk
	// So we'll duplicate the logic or use a shared constant
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".orla", "tools"), nil
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

	// Validate agent mode configuration (RFC 4)
	// Set default model if not specified
	if cfg.Model == "" {
		cfg.Model = "ollama:ministral-3:8b"
	}

	// Set default max_tool_calls if not specified
	if cfg.MaxToolCalls == 0 {
		cfg.MaxToolCalls = 10
	}

	// Validate max_tool_calls
	if cfg.MaxToolCalls < 1 {
		return fmt.Errorf("max_tool_calls must be at least 1, got %d", cfg.MaxToolCalls)
	}

	// Set default output_format if not specified
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = OrlaOutputFormatAuto
	}

	// Validate output_format
	validOutputFormats := map[OrlaOutputFormat]struct{}{
		OrlaOutputFormatAuto:  {},
		OrlaOutputFormatRich:  {},
		OrlaOutputFormatPlain: {},
	}

	if _, ok := validOutputFormats[cfg.OutputFormat]; !ok {
		return fmt.Errorf("output_format must be one of: auto, rich, plain, got '%s'", cfg.OutputFormat)
	}

	return nil
}
