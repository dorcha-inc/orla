// Package config provides configuration management for Orla: load from a
// single config file path, or use defaults when path is empty.
package config

import (
	"fmt"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	DefaultModel = "ollama:qwen3:0.6b"
)

type OrlaLogLevel string

const (
	OrlaLogLevelDebug OrlaLogLevel = "debug"
	OrlaLogLevelInfo  OrlaLogLevel = "info"
	OrlaLogLevelWarn  OrlaLogLevel = "warn"
	OrlaLogLevelError OrlaLogLevel = "error"
	OrlaLogLevelFatal OrlaLogLevel = "fatal"
)

var validLogLevels = map[OrlaLogLevel]struct{}{
	OrlaLogLevelDebug: {},
	OrlaLogLevelInfo:  {},
	OrlaLogLevelWarn:  {},
	OrlaLogLevelError: {},
	OrlaLogLevelFatal: {},
}

func IsValidMapKey[K comparable, V any](m map[K]V, key K) bool {
	_, ok := m[key]
	return ok
}

// OrlaOutputFormat represents the output format for agent mode
type OrlaOutputFormat string

const (
	OrlaOutputFormatAuto  OrlaOutputFormat = "auto"
	OrlaOutputFormatRich  OrlaOutputFormat = "rich"
	OrlaOutputFormatPlain OrlaOutputFormat = "plain"
)

var validOutputFormats = map[OrlaOutputFormat]struct{}{
	OrlaOutputFormatAuto:  {},
	OrlaOutputFormatRich:  {},
	OrlaOutputFormatPlain: {},
}

type OrlaLogFormat string

const (
	OrlaLogFormatPretty OrlaLogFormat = "pretty"
	OrlaLogFormatJSON   OrlaLogFormat = "json"
)

var validLogFormats = map[OrlaLogFormat]struct{}{
	OrlaLogFormatPretty: {},
	OrlaLogFormatJSON:   {},
}

// OrlaConfig represents the orla configuration for agent mode (and minimal shared settings).
// TODO(jadidbourbaki): move agent and service config to separate structs.
type OrlaConfig struct {
	// Service only
	ListenAddress string `yaml:"listen_address,omitempty" mapstructure:"listen_address"` // address to bind (e.g. "localhost:8081", ":8081")
	// Common to both service and agent
	LogFormat OrlaLogFormat `yaml:"log_format,omitempty" mapstructure:"log_format"` // the log format, "pretty" or "json"
	LogLevel  string        `yaml:"log_level,omitempty" mapstructure:"log_level"`   // the log level, "debug", "info", "warn", "error", "fatal"
	// Agent only (ignored by orla serve)
	LLMBackend   *core.LLMBackend `yaml:"llm_backend,omitempty" mapstructure:"llm_backend"`     // LLM backend configuration (endpoint, type, api_key)
	Model        string           `yaml:"model,omitempty" mapstructure:"model"`                 // model identifier (e.g., "ollama:ministral-3:8b", "openai:gpt-4")
	Streaming    bool             `yaml:"streaming,omitempty" mapstructure:"streaming"`         // enable streaming responses
	OutputFormat OrlaOutputFormat `yaml:"output_format,omitempty" mapstructure:"output_format"` // output format: "auto", "rich", or "plain"
	ShowThinking bool             `yaml:"show_thinking,omitempty" mapstructure:"show_thinking"` // show thinking trace output (for thinking-capable models)
	ShowProgress bool             `yaml:"show_progress,omitempty" mapstructure:"show_progress"` // show progress messages even when UI is disabled (e.g., when stdin is piped)
	SystemPrompt string           `yaml:"system_prompt,omitempty" mapstructure:"system_prompt"` // custom system prompt
}

// setupViper sets defaults and, if configPath is non-empty, reads that file only.
func setupViper(configPath string) error {
	viper.Reset()
	setViperDefaults()

	if configPath == "" {
		zap.L().Debug("no config path provided, using defaults")
		return nil
	}

	viper.SetConfigFile(configPath)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	return nil
}

// setViperDefaults sets default values in Viper
func setViperDefaults() {
	viper.SetDefault("listen_address", "localhost:8081")
	viper.SetDefault("log_format", "json")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("model", DefaultModel)
	viper.SetDefault("auto_start_ollama", true)
	viper.SetDefault("auto_configure_ollama_service", false)
	viper.SetDefault("streaming", true)
	viper.SetDefault("output_format", "auto")
	viper.SetDefault("show_thinking", false)
	viper.SetDefault("show_progress", false)
}

// LoadConfig loads configuration from a single file path. If configPath is empty,
// returns defaults only (no file read).
func LoadConfig(configPath string) (*OrlaConfig, error) {
	setupViperErr := setupViper(configPath)
	if setupViperErr != nil {
		return nil, setupViperErr
	}

	cfg := &OrlaConfig{}
	unmarshalErr := viper.Unmarshal(cfg)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", unmarshalErr)
	}

	validateConfigErr := validateConfig(cfg)
	if validateConfigErr != nil {
		return nil, validateConfigErr
	}

	return cfg, nil
}

func validateConfig(cfg *OrlaConfig) error {
	if !IsValidMapKey(validLogFormats, cfg.LogFormat) {
		return fmt.Errorf("log_format must be one of: %s, got '%s'", core.JoinMapKeys(validLogFormats), cfg.LogFormat)
	}

	if !IsValidMapKey(validLogLevels, OrlaLogLevel(cfg.LogLevel)) {
		return fmt.Errorf("log_level must be one of: %s, got '%s'", core.JoinMapKeys(validLogLevels), cfg.LogLevel)
	}

	if !IsValidMapKey(validOutputFormats, cfg.OutputFormat) {
		return fmt.Errorf("output_format must be one of: %s, got '%s'", core.JoinMapKeys(validOutputFormats), cfg.OutputFormat)
	}

	return nil
}
