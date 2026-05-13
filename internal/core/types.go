// Package core implements the core functionality for orla that is shared across all components.
package core

// DefaultBackendQueueCapacity is the queue size used when QueueCapacity is nil or <= 0.
const DefaultBackendQueueCapacity = 4096

// CostModel holds per-backend token pricing in USD per million tokens.
type CostModel struct {
	InputCostPerMToken  float64 `yaml:"input_cost_per_mtoken,omitempty" mapstructure:"input_cost_per_mtoken"`
	OutputCostPerMToken float64 `yaml:"output_cost_per_mtoken,omitempty" mapstructure:"output_cost_per_mtoken"`
}

// LLMBackend represents an LLM inference server. The provider implementation
// (and any provider-specific behavior like SGLang's cache flush) is selected
// by the prefix on the backend's model identifier — see internal/model.
type LLMBackend struct {
	// Endpoint is the URL of the LLM inference server.
	Endpoint string `yaml:"endpoint,omitempty" mapstructure:"endpoint"`
	// APIKeyEnvVar is the *ENVIRONMENT VARIABLE* storing the API key for the LLM inference API.
	// Orla *does not* allow you to store the API key in the config file. You must use an environment variable.
	APIKeyEnvVar string `yaml:"api_key_env_var,omitempty" mapstructure:"api_key_env_var"`
	// MaxConcurrency is the maximum number of concurrent inference requests dispatched
	// to this backend. nil means use default (1 — serial dispatch).
	MaxConcurrency *int `yaml:"max_concurrency,omitempty" mapstructure:"max_concurrency"`
	// QueueCapacity is the maximum number of requests that may be queued for this backend.
	// nil means use default (DefaultBackendQueueCapacity).
	QueueCapacity *int `yaml:"queue_capacity,omitempty" mapstructure:"queue_capacity"`
	// CostModel holds optional token pricing. Used by the platform engineer's
	// optimizer as a cost prior; persisted with the backend record.
	CostModel *CostModel `yaml:"cost_model,omitempty" mapstructure:"cost_model"`
	// Quality is a relative capability score in [0.0, 1.0]. Used by the
	// platform engineer's optimizer as a capability prior; persisted with the
	// backend record.
	Quality *float64 `yaml:"quality,omitempty" mapstructure:"quality"`
}

// EffectiveMaxConcurrency returns the configured max concurrency, or 1 if unset or invalid.
func (b *LLMBackend) EffectiveMaxConcurrency() int {
	if b.MaxConcurrency == nil || *b.MaxConcurrency < 1 {
		return 1
	}
	return *b.MaxConcurrency
}

// EffectiveQueueCapacity returns the configured queue capacity, or DefaultBackendQueueCapacity if unset or invalid.
func (b *LLMBackend) EffectiveQueueCapacity() int {
	if b.QueueCapacity == nil || *b.QueueCapacity <= 0 {
		return DefaultBackendQueueCapacity
	}
	return *b.QueueCapacity
}
