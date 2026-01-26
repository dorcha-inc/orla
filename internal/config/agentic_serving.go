// Package config provides configuration management for Orla, including
// Agentic Serving Layer configuration (RFC 5).
package config

import (
	"fmt"

	"github.com/dorcha-inc/orla/internal/core"
)

// AgenticServingMode represents the operational mode of the Agentic Serving Layer
type AgenticServingMode string

const (
	// AgenticServingModeEmbedded runs the serving layer in-process with orla agent
	AgenticServingModeEmbedded AgenticServingMode = "embedded"
	// AgenticServingModeDaemon runs the serving layer as a separate daemon process
	AgenticServingModeDaemon AgenticServingMode = "daemon"
)

// CachePolicyType represents the type of KV cache policy
type CachePolicyType string

const (
	// CachePolicyPreserve always preserves cache (unless flush_after_final modifier is set)
	CachePolicyPreserve CachePolicyType = "preserve"
	// CachePolicyPreserveOnSmallTurns preserves cache when context delta is small
	CachePolicyPreserveOnSmallTurns CachePolicyType = "preserve_on_small_turns"
	// CachePolicyFlushUnderPressure flushes cache when memory pressure exceeds threshold
	CachePolicyFlushUnderPressure CachePolicyType = "flush_under_pressure"
	// CachePolicyAggressiveFlush always flushes cache after each request
	CachePolicyAggressiveFlush CachePolicyType = "aggressive_flush"
	// CachePolicyPreserveWithinWorkflow preserves cache within a workflow but flushes when transitioning to a different workflow
	CachePolicyPreserveWithinWorkflow CachePolicyType = "preserve_within_workflow"
)

// AgenticServingConfig represents the Agentic Serving Layer configuration (RFC 5)
type AgenticServingConfig struct {
	// Mode is the operational mode (embedded or daemon)
	Mode AgenticServingMode `yaml:"mode,omitempty" mapstructure:"mode"`
	// Daemon configuration (only when mode=daemon)
	Daemon *DaemonConfig `yaml:"daemon,omitempty" mapstructure:"daemon"`
	// LLMServers is a list of LLM server configurations
	LLMServers []*LLMServerConfig `yaml:"llm_servers,omitempty" mapstructure:"llm_servers"`
	// AgentProfiles is a list of agent profile configurations
	AgentProfiles []*AgentProfile `yaml:"agent_profiles,omitempty" mapstructure:"agent_profiles"`
	// Workflows is a list of workflow configurations
	Workflows []*Workflow `yaml:"workflows,omitempty" mapstructure:"workflows"`
}

// DaemonConfig represents daemon-specific configuration
type DaemonConfig struct {
	// ListenAddress is the address the daemon listens on (e.g., "localhost:8081")
	ListenAddress string `yaml:"listen_address,omitempty" mapstructure:"listen_address"`
}

// LLMServerConfig represents an LLM server configuration
type LLMServerConfig struct {
	// Name is a unique identifier for this LLM server configuration
	Name string `yaml:"name" mapstructure:"name"`
	// Backend is the LLM backend configuration
	Backend *core.LLMBackend `yaml:"backend" mapstructure:"backend"`
	// Model is the model identifier (e.g., "ollama:llama3:8b", "openai:gpt-4")
	Model string `yaml:"model" mapstructure:"model"`
	// Context is the context sharing configuration
	Context *ContextConfig `yaml:"context,omitempty" mapstructure:"context"`
	// Cache is the KV cache policy configuration
	Cache *CacheConfig `yaml:"cache,omitempty" mapstructure:"cache"`
}

// ContextConfig represents context sharing configuration
type ContextConfig struct {
	// Shared indicates whether multiple agents share conversation context
	Shared bool `yaml:"shared,omitempty" mapstructure:"shared"`
	// SyncInterval is how often to synchronize context across agents (in tokens)
	// Only used when shared=true
	SyncInterval int `yaml:"sync_interval,omitempty" mapstructure:"sync_interval"`
}

// CacheConfig represents KV cache policy configuration
type CacheConfig struct {
	// Policy is the cache policy type
	Policy CachePolicyType `yaml:"policy" mapstructure:"policy"`
	// SmallTurnThreshold is the token threshold for small turns (for preserve_on_small_turns policy)
	SmallTurnThreshold int `yaml:"small_turn_threshold,omitempty" mapstructure:"small_turn_threshold"`
	// FlushUnderPressure indicates whether to flush under memory pressure
	FlushUnderPressure bool `yaml:"flush_under_pressure,omitempty" mapstructure:"flush_under_pressure"`
	// MemoryPressureThreshold is the memory pressure threshold (0.0-1.0) for flush_under_pressure policy
	MemoryPressureThreshold float64 `yaml:"memory_pressure_threshold,omitempty" mapstructure:"memory_pressure_threshold"`
	// FlushAfterFinal indicates whether to flush cache after final iteration
	FlushAfterFinal bool `yaml:"flush_after_final,omitempty" mapstructure:"flush_after_final"`
}

// AgentProfile represents an agent profile configuration
type AgentProfile struct {
	// Name is a unique identifier for this agent profile
	Name string `yaml:"name" mapstructure:"name"`
	// LLMServer is the name of the LLM server configuration to use
	LLMServer string `yaml:"llm_server" mapstructure:"llm_server"`
	// Inference is the inference parameter configuration
	Inference *InferenceConfig `yaml:"inference,omitempty" mapstructure:"inference"`
	// Tools is the tool access configuration
	Tools *ToolsConfig `yaml:"tools,omitempty" mapstructure:"tools"`
}

// InferenceConfig represents inference parameter configuration
type InferenceConfig struct {
	// Temperature is the sampling temperature (0.0-2.0)
	Temperature float64 `yaml:"temperature,omitempty" mapstructure:"temperature"`
	// TopP is the nucleus sampling parameter (0.0-1.0)
	TopP float64 `yaml:"top_p,omitempty" mapstructure:"top_p"`
	// MaxTokens is the maximum number of tokens to generate (nil = no limit)
	MaxTokens *int `yaml:"max_tokens,omitempty" mapstructure:"max_tokens"`
}

// ToolsConfig represents tool access configuration
type ToolsConfig struct {
	// Allowed is a list of allowed tool names (empty = all tools available)
	Allowed []string `yaml:"allowed,omitempty" mapstructure:"allowed"`
}

// Workflow represents a workflow configuration
type Workflow struct {
	// Name is a unique identifier for this workflow
	Name string `yaml:"name" mapstructure:"name"`
	// Tasks is a sequence of workflow tasks
	Tasks []*WorkflowTask `yaml:"tasks" mapstructure:"tasks"`
}

// WorkflowTask represents a single task in a workflow
type WorkflowTask struct {
	// AgentProfile is the name of the agent profile to use for this task
	AgentProfile string `yaml:"agent_profile" mapstructure:"agent_profile"`
	// LLMServer is an optional override for the LLM server configuration
	// If not set, uses the LLM server from the agent profile
	LLMServer string `yaml:"llm_server,omitempty" mapstructure:"llm_server"`
	// Turn is the turn number for multi-agent coordination (1-based)
	Turn int `yaml:"turn,omitempty" mapstructure:"turn"`
	// Prompt is the prompt or prompt template for this task
	// If empty, uses the workflow's initial prompt
	Prompt string `yaml:"prompt,omitempty" mapstructure:"prompt"`
	// UseContext indicates whether to use previous task outputs as context
	// When true, the task receives the accumulated context from previous tasks
	UseContext bool `yaml:"use_context,omitempty" mapstructure:"use_context"`
}

// validateAgenticServingConfig validates the agentic serving configuration
func validateAgenticServingConfig(cfg *AgenticServingConfig) error {
	// Validate mode
	if cfg.Mode != "" && cfg.Mode != AgenticServingModeEmbedded && cfg.Mode != AgenticServingModeDaemon {
		return fmt.Errorf("mode must be 'embedded' or 'daemon', got '%s'", cfg.Mode)
	}

	// Validate daemon config (only when mode=daemon)
	if cfg.Mode == AgenticServingModeDaemon && cfg.Daemon == nil {
		return fmt.Errorf("daemon configuration is required when mode=daemon")
	}

	// Validate LLM server name uniqueness
	llmServerNames := make(map[string]bool)
	for _, server := range cfg.LLMServers {
		if server.Name == "" {
			return fmt.Errorf("llm_server name cannot be empty")
		}
		if llmServerNames[server.Name] {
			return fmt.Errorf("duplicate llm_server name: %s", server.Name)
		}
		llmServerNames[server.Name] = true

		// Validate LLM server configuration
		if err := validateLLMServerConfig(server); err != nil {
			return fmt.Errorf("llm_server '%s': %w", server.Name, err)
		}
	}

	// Validate agent profile name uniqueness and references
	agentProfileNames := make(map[string]bool)
	for _, profile := range cfg.AgentProfiles {
		if profile.Name == "" {
			return fmt.Errorf("agent_profile name cannot be empty")
		}
		if agentProfileNames[profile.Name] {
			return fmt.Errorf("duplicate agent_profile name: %s", profile.Name)
		}
		agentProfileNames[profile.Name] = true

		// Validate agent profile references
		if profile.LLMServer == "" {
			return fmt.Errorf("agent_profile '%s': llm_server cannot be empty", profile.Name)
		}
		if !llmServerNames[profile.LLMServer] {
			return fmt.Errorf("agent_profile '%s': references unknown llm_server '%s'", profile.Name, profile.LLMServer)
		}

		// Validate inference config if present
		if profile.Inference != nil {
			if err := validateInferenceConfig(profile.Inference); err != nil {
				return fmt.Errorf("agent_profile '%s': %w", profile.Name, err)
			}
		}
	}

	// Validate workflow name uniqueness and references
	workflowNames := make(map[string]bool)
	for _, workflow := range cfg.Workflows {
		if workflow.Name == "" {
			return fmt.Errorf("workflow name cannot be empty")
		}
		if workflowNames[workflow.Name] {
			return fmt.Errorf("duplicate workflow name: %s", workflow.Name)
		}
		workflowNames[workflow.Name] = true

		// Validate workflow tasks
		if len(workflow.Tasks) == 0 {
			return fmt.Errorf("workflow '%s': must have at least one task", workflow.Name)
		}
		for i, task := range workflow.Tasks {
			if task.AgentProfile == "" {
				return fmt.Errorf("workflow '%s' task %d: agent_profile cannot be empty", workflow.Name, i+1)
			}
			if !agentProfileNames[task.AgentProfile] {
				return fmt.Errorf("workflow '%s' task %d: references unknown agent_profile '%s'", workflow.Name, i+1, task.AgentProfile)
			}
			// Validate optional LLM server override
			if task.LLMServer != "" && !llmServerNames[task.LLMServer] {
				return fmt.Errorf("workflow '%s' task %d: references unknown llm_server '%s'", workflow.Name, i+1, task.LLMServer)
			}
		}
	}

	return nil
}

// validateLLMServerConfig validates an LLM server configuration
func validateLLMServerConfig(server *LLMServerConfig) error {
	if server.Backend == nil {
		return fmt.Errorf("backend cannot be nil")
	}
	if server.Backend.Type != core.LLMInferenceAPITypeOllama && server.Backend.Type != core.LLMInferenceAPITypeOpenAI && server.Backend.Type != core.LLMInferenceAPITypeSGLang {
		return fmt.Errorf("backend type must be 'ollama', 'openai', or 'sglang', got '%s'", server.Backend.Type)
	}
	if server.Backend.Endpoint == "" {
		return fmt.Errorf("backend endpoint cannot be empty")
	}
	if server.Model == "" {
		return fmt.Errorf("model cannot be empty")
	}

	// Validate context config if present
	if server.Context != nil {
		if server.Context.Shared && server.Context.SyncInterval < 0 {
			return fmt.Errorf("sync_interval must be non-negative when shared=true")
		}
	}

	// Validate cache config if present
	if server.Cache != nil {
		if err := validateCacheConfig(server.Cache); err != nil {
			return err
		}
	}

	return nil
}

// validateCacheConfig validates a cache configuration
func validateCacheConfig(cache *CacheConfig) error {
	validPolicies := map[CachePolicyType]struct{}{
		CachePolicyPreserve:              {},
		CachePolicyPreserveOnSmallTurns:  {},
		CachePolicyFlushUnderPressure:    {},
		CachePolicyAggressiveFlush:       {},
		CachePolicyPreserveWithinWorkflow: {},
	}

	if _, ok := validPolicies[cache.Policy]; !ok {
		return fmt.Errorf("cache policy must be one of: preserve, preserve_on_small_turns, flush_under_pressure, aggressive_flush, got '%s'", cache.Policy)
	}

	// Validate policy-specific parameters
	switch cache.Policy {
	case CachePolicyPreserveOnSmallTurns:
		if cache.SmallTurnThreshold < 0 {
			return fmt.Errorf("small_turn_threshold must be non-negative")
		}
	case CachePolicyFlushUnderPressure:
		if cache.MemoryPressureThreshold < 0 || cache.MemoryPressureThreshold > 1 {
			return fmt.Errorf("memory_pressure_threshold must be between 0.0 and 1.0, got %f", cache.MemoryPressureThreshold)
		}
	}

	return nil
}

// validateInferenceConfig validates an inference configuration
func validateInferenceConfig(inference *InferenceConfig) error {
	if inference.Temperature < 0 || inference.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0.0 and 2.0, got %f", inference.Temperature)
	}
	if inference.TopP < 0 || inference.TopP > 1 {
		return fmt.Errorf("top_p must be between 0.0 and 1.0, got %f", inference.TopP)
	}
	if inference.MaxTokens != nil && *inference.MaxTokens < 1 {
		return fmt.Errorf("max_tokens must be at least 1, got %d", *inference.MaxTokens)
	}
	return nil
}
