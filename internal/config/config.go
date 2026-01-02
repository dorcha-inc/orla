// Package config provides configuration management for Orla, including
// loading configuration with precedence, environment variable overrides,
// and get/set/list operations for configuration values (RFC 4).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/dorcha-inc/orla/internal/state"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

const (
	DefaultToolsDir     = ".orla/tools"
	DefaultModel        = "ollama:qwen3:1.7b"
	DefaultMaxToolCalls = 10
)

type OrlaLogLevel string

const (
	OrlaLogLevelDebug OrlaLogLevel = "debug"
	OrlaLogLevelInfo  OrlaLogLevel = "info"
	OrlaLogLevelWarn  OrlaLogLevel = "warn"
	OrlaLogLevelError OrlaLogLevel = "error"
	OrlaLogLevelFatal OrlaLogLevel = "fatal"
)

func ValidLogLevels() map[OrlaLogLevel]struct{} {
	return map[OrlaLogLevel]struct{}{
		OrlaLogLevelDebug: {},
		OrlaLogLevelInfo:  {},
		OrlaLogLevelWarn:  {},
		OrlaLogLevelError: {},
		OrlaLogLevelFatal: {},
	}
}

func IsValidLogLevel(level OrlaLogLevel) bool {
	validLogLevels := ValidLogLevels()
	_, ok := validLogLevels[level]
	return ok
}

// OrlaOutputFormat represents the output format for agent mode
type OrlaOutputFormat string

const (
	OrlaOutputFormatAuto  OrlaOutputFormat = "auto"
	OrlaOutputFormatRich  OrlaOutputFormat = "rich"
	OrlaOutputFormatPlain OrlaOutputFormat = "plain"
)

func ValidOutputFormats() map[OrlaOutputFormat]struct{} {
	return map[OrlaOutputFormat]struct{}{
		OrlaOutputFormatAuto:  {},
		OrlaOutputFormatRich:  {},
		OrlaOutputFormatPlain: {},
	}
}

func IsValidOutputFormat(format OrlaOutputFormat) bool {
	_, ok := ValidOutputFormats()[format]
	return ok
}

type OrlaLogFormat string

const (
	OrlaLogFormatPretty OrlaLogFormat = "pretty"
	OrlaLogFormatJSON   OrlaLogFormat = "json"
)

func ValidLogFormats() map[OrlaLogFormat]struct{} {
	return map[OrlaLogFormat]struct{}{
		OrlaLogFormatPretty: {},
		OrlaLogFormatJSON:   {},
	}
}

func IsValidLogFormat(format OrlaLogFormat) bool {
	_, ok := ValidLogFormats()[format]
	return ok
}

// OrlaConfig represents the orla configuration, including
// the tools directory, the port to listen on, the timeout
// for tool executions, the log format, and the log level.
// It also includes Agent Mode configuration (RFC 4).
type OrlaConfig struct {
	// Server mode configuration (RFC 1)
	ToolsDir      string               `yaml:"tools_dir,omitempty" mapstructure:"tools_dir"`           // the directory containing the tools
	ToolsRegistry *state.ToolsRegistry `yaml:"tools_registry,omitempty" mapstructure:"tools_registry"` // the tools registry
	Port          int                  `yaml:"port,omitempty" mapstructure:"port"`                     // the port to listen on
	Timeout       int                  `yaml:"timeout,omitempty" mapstructure:"timeout"`               // the timeout for tool executions in seconds
	LogFormat     OrlaLogFormat        `yaml:"log_format,omitempty" mapstructure:"log_format"`         // the log format, "pretty" or "json"
	LogLevel      string               `yaml:"log_level,omitempty" mapstructure:"log_level"`           // the log level, "debug", "info", "warn", "error", "fatal"
	LogFile       string               `yaml:"log_file,omitempty" mapstructure:"log_file"`             // optional log file path

	// Agent mode configuration (RFC 4)
	Model              string           `yaml:"model,omitempty" mapstructure:"model"`                             // model identifier (e.g., "ollama:ministral-3:8b", "openai:gpt-4")
	MaxToolCalls       int              `yaml:"max_tool_calls,omitempty" mapstructure:"max_tool_calls"`           // maximum tool calls per prompt
	Streaming          bool             `yaml:"streaming,omitempty" mapstructure:"streaming"`                     // enable streaming responses
	OutputFormat       OrlaOutputFormat `yaml:"output_format,omitempty" mapstructure:"output_format"`             // output format: "auto", "rich", or "plain"
	ConfirmDestructive bool             `yaml:"confirm_destructive,omitempty" mapstructure:"confirm_destructive"` // prompt for destructive actions
	DryRun             bool             `yaml:"dry_run,omitempty" mapstructure:"dry_run"`                         // default to non-dry-run mode
	ShowThinking       bool             `yaml:"show_thinking,omitempty" mapstructure:"show_thinking"`             // show thinking trace output (for thinking-capable models)
	ShowToolCalls      bool             `yaml:"show_tool_calls,omitempty" mapstructure:"show_tool_calls"`         // show detailed tool call information
	ShowProgress       bool             `yaml:"show_progress,omitempty" mapstructure:"show_progress"`             // show progress messages even when UI is disabled (e.g., when stdin is piped)
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
	// Scan for direct executables in ToolsDir (flat structure for RFC 1 backward compatibility)
	dirTools, err := state.ScanToolsFromDirectory(cfg.ToolsDir)
	if err != nil {
		return err
	}

	// Scan for installed tools in ToolsDir (TOOL-NAME/VERSION/ structure from RFC 3)
	// Both scan the same directory but different patterns:
	// - ScanToolsFromDirectory: flat executables (tools/hello.sh)
	// - ScanInstalledTools: installed tools (tools/my-tool/1.0.0/tool.yaml)
	// They only conflict if a flat executable has the same name as an installed tool
	if cfg.ToolsDir != "" {
		installedTools, err := state.ScanInstalledTools(cfg.ToolsDir)
		if err != nil {
			zap.L().Warn("Failed to scan installed tools in ToolsDir", zap.String("dir", cfg.ToolsDir), zap.Error(err))
		} else {
			// Merge: installed tools take precedence over flat executables with the same name
			for name, tool := range installedTools {
				if _, exists := dirTools[name]; exists {
					zap.L().Debug("Tool found in both directory and installed tools, using installed version", zap.String("tool", name))
				}
				dirTools[name] = tool
			}
		}
	}

	cfg.ToolsRegistry = &state.ToolsRegistry{Tools: dirTools}
	return nil
}

// ConfigValue represents a configuration value with its source
type ConfigValue struct {
	Value  any
	Source string // "env", "project", "user", or "default"
}

// GetUserConfigPath returns the path to the user-specific config file (~/.orla/config.yaml)
func GetUserConfigPath() (string, error) {
	orlaHome, err := registry.GetOrlaHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get orla home directory: %w", err)
	}
	return filepath.Join(orlaHome, "config.yaml"), nil
}

// GetProjectConfigPath returns the path to the project-specific config file (./orla.yaml)
// relative to the current working directory
func GetProjectConfigPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	return filepath.Join(cwd, "orla.yaml"), nil
}

// setupViper configures Viper with defaults, config file locations, and environment variables
// If configPath is provided (non-empty), loads from that specific path instead of using precedence
func setupViper(configPath string) error {
	viper.Reset()
	setViperDefaults()
	viper.SetEnvPrefix("ORLA")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	// If specific path provided, load only that file
	if configPath != "" {
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		return nil
	}

	// Otherwise use precedence: user config first, then project config
	userPath, userErr := GetUserConfigPath()
	if userErr == nil {
		if _, userStatErr := os.Stat(userPath); userStatErr == nil {
			viper.SetConfigFile(userPath)
			if userReadErr := viper.ReadInConfig(); userReadErr != nil {
				zap.L().Debug("Failed to read user config file", zap.String("path", userPath), zap.Error(userReadErr))
			}
		}
	}

	projectPath, projectErr := GetProjectConfigPath()
	if projectErr == nil {
		if _, projectStatErr := os.Stat(projectPath); projectStatErr == nil {
			viper.SetConfigFile(projectPath)
			if projectReadErr := viper.MergeInConfig(); projectReadErr != nil {
				zap.L().Debug("Failed to merge project config file", zap.String("path", projectPath), zap.Error(projectReadErr))
			}
		}
	}

	return nil
}

// setViperDefaults sets default values in Viper
func setViperDefaults() {
	// Server mode defaults
	viper.SetDefault("port", 8080)
	viper.SetDefault("timeout", 30)
	viper.SetDefault("log_format", "json")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_file", "")

	// Agent mode defaults
	viper.SetDefault("model", DefaultModel)
	viper.SetDefault("auto_start_ollama", true)
	viper.SetDefault("auto_configure_ollama_service", false)
	viper.SetDefault("max_tool_calls", DefaultMaxToolCalls)
	viper.SetDefault("streaming", true)
	viper.SetDefault("output_format", "auto")
	viper.SetDefault("confirm_destructive", true)
	viper.SetDefault("dry_run", false)
	viper.SetDefault("show_thinking", false)
	viper.SetDefault("show_tool_calls", false)
	viper.SetDefault("show_progress", false)
}

// LoadConfig loads configuration with precedence: project config > user config > defaults
// Environment variables override config file values
// If configPath is provided, loads from that specific path instead
func LoadConfig(configPath string) (*OrlaConfig, error) {
	if err := setupViper(configPath); err != nil {
		return nil, err
	}

	// Unmarshal from Viper
	cfg := &OrlaConfig{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Post-process: handle ToolsRegistry and tools directory
	var configFileDir string
	if configPath != "" {
		configFileDir = filepath.Dir(configPath)
	} else {
		// Check if project config exists (for determining default tools_dir)
		projectPath, err := GetProjectConfigPath()
		if err == nil {
			if _, err := os.Stat(projectPath); err == nil {
				configFileDir = filepath.Dir(projectPath)
			}
		}
	}

	if err := postProcessConfig(cfg, configFileDir); err != nil {
		return nil, err
	}

	// Validate
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// postProcessConfig handles ToolsRegistry resolution and tools directory setup
func postProcessConfig(cfg *OrlaConfig, configFileDir string) error {
	// Handle ToolsRegistry special case: if tools_registry is explicitly set in config, use it
	// Check if tools_registry was set in the config (not just default empty value)
	if cfg.ToolsRegistry != nil && len(cfg.ToolsRegistry.Tools) > 0 {
		if configFileDir == "" {
			return fmt.Errorf("config file directory is not set but ToolsRegistry is set")
		}

		// Resolve relative paths in ToolsRegistry relative to config file
		for _, tool := range cfg.ToolsRegistry.Tools {
			if tool.Path != "" && !filepath.IsAbs(tool.Path) {
				absPath, err := filepath.Abs(filepath.Join(configFileDir, tool.Path))
				if err != nil {
					return fmt.Errorf("failed to resolve tool path: %w", err)
				}
				tool.Path = absPath
			}
			if tool.Path == "" && tool.Entrypoint != "" {
				absPath, err := filepath.Abs(filepath.Join(configFileDir, tool.Entrypoint))
				if err != nil {
					return fmt.Errorf("failed to resolve entrypoint path: %w", err)
				}
				tool.Path = absPath
			}
		}
		// ToolsRegistry is set, no need to rebuild registry
		return nil
	}

	// Set tools directory (handles path resolution and registry rebuild)
	toolsDir := cfg.ToolsDir

	// If toolsDir is empty (not set in config), use global ~/.orla/tools
	if toolsDir == "" {
		orlaHome, err := registry.GetOrlaHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get orla home directory: %w", err)
		}
		toolsDir = filepath.Join(orlaHome, "tools")
		zap.L().Debug("tools_dir not set in config, using global tools directory", zap.String("tools_dir", toolsDir))
	}

	// Resolve relative paths relative to the appropriate config directory
	if !filepath.IsAbs(toolsDir) {
		if configFileDir != "" {
			// Project config exists: resolve relative to project directory
			toolsDir = filepath.Join(configFileDir, toolsDir)
		} else {
			// No project config: resolve relative to user config directory (~/.orla/)
			// This handles both the default ".orla/tools" and user-specified relative paths
			homeDir, homeDirErr := registry.GetOrlaHomeDir()
			if homeDirErr != nil {
				return fmt.Errorf("failed to get orla home directory: %w", homeDirErr)
			}

			if toolsDir == DefaultToolsDir {
				toolsDir = "tools"
			}

			toolsDir = filepath.Join(homeDir, toolsDir)
		}

		// Clean the final path to normalize any remaining ./ or ../ components
		toolsDir = filepath.Clean(toolsDir)
	}

	if err := cfg.SetToolsDir(toolsDir); err != nil {
		return fmt.Errorf("failed to set tools directory: %w", err)
	}

	return nil
}

// validateConfig validates the configuration
// Note: This function can be called both:
// 1. After LoadConfig() (viper is configured) - can use viper.IsSet() to detect explicit values
// 2. Directly on structs (viper not configured) - viper.IsSet() may not work, so we apply defaults
func validateConfig(cfg *OrlaConfig) error {
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535, got %d", cfg.Port)
	}
	if cfg.Timeout < 1 {
		return fmt.Errorf("timeout must be at least 1 second, got %d", cfg.Timeout)
	}

	if cfg.LogFormat != "" && !IsValidLogFormat(cfg.LogFormat) {
		return fmt.Errorf("log_format must be one of: %s, got '%s'", core.JoinMapKeys(ValidLogFormats()), cfg.LogFormat)
	}
	if cfg.LogLevel != "" && !IsValidLogLevel(OrlaLogLevel(cfg.LogLevel)) {
		return fmt.Errorf("log_level must be one of: %s, got '%s'", core.JoinMapKeys(ValidLogLevels()), cfg.LogLevel)
	}

	// Since viper handles defaults, these are values that were explicitly set to empty or zero
	// and need to be validated.
	if cfg.Model == "" {
		return fmt.Errorf("model cannot be empty (was explicitly set to empty string)")
	}

	if cfg.MaxToolCalls < 1 {
		return fmt.Errorf("max_tool_calls must be at least 1, got %d", cfg.MaxToolCalls)
	}

	if !IsValidOutputFormat(cfg.OutputFormat) {
		return fmt.Errorf("output_format must be one of: %s, got '%s'", core.JoinMapKeys(ValidOutputFormats()), cfg.OutputFormat)
	}

	return nil
}

// getValueSource determines the source of a config value
func getValueSource(key string) string {
	// Check if environment variable is set
	envKey := "ORLA_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	if os.Getenv(envKey) != "" {
		return "env"
	}

	// Check project config
	projectPath, err := GetProjectConfigPath()
	if err == nil {
		if _, projectStatErr := os.Stat(projectPath); projectStatErr == nil {
			if viper.IsSet(key) {
				// Check if this value came from project config
				// Viper doesn't track source, so we check if project config has the key
				projectViper := viper.New()
				projectViper.SetConfigFile(projectPath)
				if projectReadErr := projectViper.ReadInConfig(); projectReadErr == nil {
					if projectViper.IsSet(key) {
						return "project"
					}
				}
			}
		}
	}

	// Check user config
	userPath, userErr := GetUserConfigPath()
	if userErr == nil {
		if _, userStatErr := os.Stat(userPath); userStatErr == nil {
			userViper := viper.New()
			userViper.SetConfigFile(userPath)
			if userReadErr := userViper.ReadInConfig(); userReadErr == nil {
				if userViper.IsSet(key) {
					return "user"
				}
			}
		}
	}

	return "default"
}

// GetConfigValue retrieves a configuration value by key, checking environment variables first
// Returns the value and its source ("env", "project", "user", or "default")
func GetConfigValue(key string) (*ConfigValue, error) {
	if err := setupViper(""); err != nil {
		return nil, err
	}

	// Viper handles defaults, so Get will return default if not set
	value := viper.Get(key)
	if value == nil {
		return nil, fmt.Errorf("unknown config key: %s", key)
	}

	source := getValueSource(key)
	return &ConfigValue{Value: value, Source: source}, nil
}

// SetConfigValue sets a configuration value and saves it to the appropriate config file
func SetConfigValue(key, value string) error {
	// Determine which config file to update
	projectPath, projectErr := GetProjectConfigPath()
	var configPath string

	if projectErr == nil {
		if _, projectStatErr := os.Stat(projectPath); projectStatErr == nil {
			configPath = projectPath
		}
	}

	if configPath == "" {
		// Use user config
		userPath, userErr := GetUserConfigPath()
		if userErr != nil {
			return fmt.Errorf("failed to get user config path: %w", userErr)
		}
		// Ensure directory exists
		configDir := filepath.Dir(userPath)
		// #nosec G301 -- config directory permissions 0755 are acceptable for user config directory
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		configPath = userPath
	}

	// Load existing config using Viper
	if err := setupViper(configPath); err != nil {
		return fmt.Errorf("failed to load existing config: %w", err)
	}

	// Set the value in Viper
	viper.Set(key, value)

	// Unmarshal into config struct
	cfg := &OrlaConfig{}
	if err := viper.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Post-process
	configFileDir := filepath.Dir(configPath)
	// #nosec G104 -- postProcessConfig errors are handled by the caller
	if err := postProcessConfig(cfg, configFileDir); err != nil {
		return err
	}

	// Save to file
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// #nosec G306 -- config file permissions 0644 are acceptable for user config files
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ListConfig returns all configuration keys and values with their sources
func ListConfig() (map[string]*ConfigValue, error) {
	if err := setupViper(""); err != nil {
		return nil, err
	}

	result := make(map[string]*ConfigValue)

	// Get all keys from Viper's AllSettings
	allSettings := viper.AllSettings()
	for key := range allSettings {
		// Skip nested maps (like tools_registry)
		if _, ok := allSettings[key].(map[string]interface{}); ok {
			continue
		}
		configVal, err := GetConfigValue(key)
		if err != nil {
			continue
		}
		result[key] = configVal
	}

	return result, nil
}
