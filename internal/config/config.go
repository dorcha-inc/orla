// Package config provides configuration management for Orla: load from a
// single config file path, or use defaults when path is empty.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/spf13/viper"
	"log/slog"
)

const (
	DefaultModel = "openai:qwen3:0.6b"
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
	RateLimitRPS  int    `yaml:"rate_limit_rps,omitempty" mapstructure:"rate_limit_rps"`   // max requests/sec for execute and backends; 0 = disabled
	StoragePath   string `yaml:"storage_path,omitempty" mapstructure:"storage_path"`       // path to the embedded SQLite database file; ":memory:" for ephemeral
	// Common to both service and agent
	LogFormat OrlaLogFormat `yaml:"log_format,omitempty" mapstructure:"log_format"` // the log format, "pretty" or "json"
	LogLevel  string        `yaml:"log_level,omitempty" mapstructure:"log_level"`   // the log level, "debug", "info", "warn", "error", "fatal"
	// Agent only (ignored by orla serve)
	LLMBackend   *core.LLMBackend `yaml:"llm_backend,omitempty" mapstructure:"llm_backend"`     // LLM backend configuration (endpoint, type, api_key)
	Model        string           `yaml:"model,omitempty" mapstructure:"model"`                 // model identifier (e.g., "openai:qwen3:0.6b", "openai:gpt-4")
	Streaming    bool             `yaml:"streaming,omitempty" mapstructure:"streaming"`         // enable streaming responses
	OutputFormat OrlaOutputFormat `yaml:"output_format,omitempty" mapstructure:"output_format"` // output format: "auto", "rich", or "plain"
	ShowThinking bool             `yaml:"show_thinking,omitempty" mapstructure:"show_thinking"` // show thinking trace output (for thinking-capable models)
	ShowProgress bool             `yaml:"show_progress,omitempty" mapstructure:"show_progress"` // show progress messages even when UI is disabled (e.g., when stdin is piped)
}

// setupViper sets defaults and, if configPath is non-empty, reads that file only.
func setupViper(configPath string) error {
	viper.Reset()
	setViperDefaults()

	if configPath == "" {
		slog.Debug("no config path provided, using defaults")
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
	viper.SetDefault("rate_limit_rps", 0)
	viper.SetDefault("log_format", "json")
	viper.SetDefault("log_level", "info")
	viper.SetDefault("model", DefaultModel)
	viper.SetDefault("streaming", true)
	viper.SetDefault("output_format", "auto")
	viper.SetDefault("show_thinking", false)
	viper.SetDefault("show_progress", false)
}

// ResolveStoragePath returns the storage path to use for the embedded SQLite
// database. If the config sets a path explicitly it is honored verbatim.
// Otherwise we use $XDG_DATA_HOME/orla/orla.db (Linux) or ~/.orla/orla.db,
// creating the parent directory if needed.
func (c *OrlaConfig) ResolveStoragePath() (string, error) {
	if c.StoragePath != "" {
		return c.StoragePath, nil
	}
	var dir string
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		dir = filepath.Join(xdg, "orla")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for default storage path: %w", err)
		}
		dir = filepath.Join(home, ".orla")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // G703 false positive; dir is from $XDG_DATA_HOME or os.UserHomeDir(), both controlled sources.
		return "", fmt.Errorf("create storage directory %q: %w", dir, err)
	}
	return filepath.Join(dir, "orla.db"), nil
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

	// Default LLMBackend for local Ollama when using default model
	if cfg.LLMBackend == nil && cfg.Model == DefaultModel {
		cfg.LLMBackend = &core.LLMBackend{Endpoint: "http://localhost:11434/v1"}
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

	if err := validateLLMBackend(cfg.LLMBackend); err != nil {
		return fmt.Errorf("llm_backend: %w", err)
	}

	return nil
}

func validateLLMBackend(b *core.LLMBackend) error {
	if b == nil {
		return nil
	}
	if b.Quality != nil {
		if q := *b.Quality; !(q >= 0 && q <= 1) {
			return fmt.Errorf("quality must be in [0.0, 1.0]; got %v", q)
		}
	}
	if b.CostModel != nil {
		in, out := b.CostModel.InputCostPerMToken, b.CostModel.OutputCostPerMToken
		if !core.IsFinite(in) || !core.IsFinite(out) || in < 0 || out < 0 {
			return fmt.Errorf("cost_model rates must be finite non-negative numbers; got input=%v, output=%v", in, out)
		}
	}
	return nil
}
