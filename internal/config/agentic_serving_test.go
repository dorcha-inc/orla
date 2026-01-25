package config

import (
	"testing"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAgenticServingConfig_ValidEmbedded(t *testing.T) {
	cfg := &AgenticServingConfig{
		Mode: AgenticServingModeEmbedded,
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{AgentProfile: "test-profile"},
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateAgenticServingConfig_ValidDaemon(t *testing.T) {
	cfg := &AgenticServingConfig{
		Mode: AgenticServingModeDaemon,
		Daemon: &DaemonConfig{
			ListenAddress: "localhost:8081",
		},
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{AgentProfile: "test-profile"},
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateAgenticServingConfig_InvalidMode(t *testing.T) {
	cfg := &AgenticServingConfig{
		Mode: AgenticServingMode("invalid"),
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mode must be 'embedded' or 'daemon'")
}

func TestValidateAgenticServingConfig_DaemonModeWithoutConfig(t *testing.T) {
	cfg := &AgenticServingConfig{
		Mode: AgenticServingModeDaemon,
		// Daemon config is nil
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "daemon configuration is required")
}

func TestValidateAgenticServingConfig_EmptyLLMServerName(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "", // Empty name
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "llm_server name cannot be empty")
}

func TestValidateAgenticServingConfig_DuplicateLLMServerName(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
			{
				Name:    "test-server", // Duplicate
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate llm_server name")
}

func TestValidateAgenticServingConfig_EmptyAgentProfileName(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "", // Empty name
				LLMServer: "test-server",
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent_profile name cannot be empty")
}

func TestValidateAgenticServingConfig_DuplicateAgentProfileName(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
			{
				Name:      "test-profile", // Duplicate
				LLMServer: "test-server",
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate agent_profile name")
}

func TestValidateAgenticServingConfig_AgentProfileMissingLLMServer(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "", // Empty
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "llm_server cannot be empty")
}

func TestValidateAgenticServingConfig_AgentProfileUnknownLLMServer(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "unknown-server", // Doesn't exist
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown llm_server")
}

func TestValidateAgenticServingConfig_EmptyWorkflowName(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "", // Empty name
				Tasks: []*WorkflowTask{
					{AgentProfile: "test-profile"},
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow name cannot be empty")
}

func TestValidateAgenticServingConfig_DuplicateWorkflowName(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{AgentProfile: "test-profile"},
				},
			},
			{
				Name: "test-workflow", // Duplicate
				Tasks: []*WorkflowTask{
					{AgentProfile: "test-profile"},
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate workflow name")
}

func TestValidateAgenticServingConfig_WorkflowNoTasks(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name:  "test-workflow",
				Tasks: []*WorkflowTask{}, // Empty tasks
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must have at least one task")
}

func TestValidateAgenticServingConfig_WorkflowTaskEmptyAgentProfile(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{AgentProfile: ""}, // Empty
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent_profile cannot be empty")
}

func TestValidateAgenticServingConfig_WorkflowTaskUnknownAgentProfile(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{AgentProfile: "unknown-profile"}, // Doesn't exist
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown agent_profile")
}

func TestValidateAgenticServingConfig_WorkflowTaskUnknownLLMServer(t *testing.T) {
	cfg := &AgenticServingConfig{
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{
						AgentProfile: "test-profile",
						LLMServer:    "unknown-server", // Doesn't exist
					},
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "references unknown llm_server")
}

func TestValidateLLMServerConfig_Valid(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
		Model:   "test-model",
	}

	err := validateLLMServerConfig(server)
	assert.NoError(t, err)
}

func TestValidateLLMServerConfig_NilBackend(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: nil,
		Model:   "test-model",
	}

	err := validateLLMServerConfig(server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backend cannot be nil")
}

func TestValidateLLMServerConfig_InvalidBackendType(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: &core.LLMBackend{Type: core.LLMInferenceAPIType("invalid"), Endpoint: "http://localhost:8080"},
		Model:   "test-model",
	}

	err := validateLLMServerConfig(server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backend type must be")
}

func TestValidateLLMServerConfig_EmptyEndpoint(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: ""},
		Model:   "test-model",
	}

	err := validateLLMServerConfig(server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backend endpoint cannot be empty")
}

func TestValidateLLMServerConfig_EmptyModel(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
		Model:   "",
	}

	err := validateLLMServerConfig(server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model cannot be empty")
}

func TestValidateLLMServerConfig_ValidBackendTypes(t *testing.T) {
	validTypes := []core.LLMInferenceAPIType{
		core.LLMInferenceAPITypeOllama,
		core.LLMInferenceAPITypeOpenAI,
		core.LLMInferenceAPITypeSGLang,
	}

	for _, backendType := range validTypes {
		server := &LLMServerConfig{
			Name:    "test-server",
			Backend: &core.LLMBackend{Type: backendType, Endpoint: "http://localhost:8080"},
			Model:   "test-model",
		}

		err := validateLLMServerConfig(server)
		assert.NoError(t, err, "Backend type %s should be valid", backendType)
	}
}

func TestValidateLLMServerConfig_ContextConfig_InvalidSyncInterval(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
		Model:   "test-model",
		Context: &ContextConfig{
			Shared:       true,
			SyncInterval: -1, // Invalid
		},
	}

	err := validateLLMServerConfig(server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync_interval must be non-negative")
}

func TestValidateLLMServerConfig_ContextConfig_Valid(t *testing.T) {
	server := &LLMServerConfig{
		Name:    "test-server",
		Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
		Model:   "test-model",
		Context: &ContextConfig{
			Shared:       true,
			SyncInterval: 100,
		},
	}

	err := validateLLMServerConfig(server)
	assert.NoError(t, err)
}

func TestValidateCacheConfig_InvalidPolicy(t *testing.T) {
	cache := &CacheConfig{
		Policy: CachePolicyType("invalid"),
	}

	err := validateCacheConfig(cache)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache policy must be one of")
}

func TestValidateCacheConfig_ValidPolicies(t *testing.T) {
	validPolicies := []CachePolicyType{
		CachePolicyPreserve,
		CachePolicyPreserveOnSmallTurns,
		CachePolicyFlushUnderPressure,
		CachePolicyAggressiveFlush,
	}

	for _, policy := range validPolicies {
		cache := &CacheConfig{
			Policy: policy,
		}

		err := validateCacheConfig(cache)
		assert.NoError(t, err, "Policy %s should be valid", policy)
	}
}

func TestValidateCacheConfig_PreserveOnSmallTurns_InvalidThreshold(t *testing.T) {
	cache := &CacheConfig{
		Policy:            CachePolicyPreserveOnSmallTurns,
		SmallTurnThreshold: -1, // Invalid
	}

	err := validateCacheConfig(cache)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "small_turn_threshold must be non-negative")
}

func TestValidateCacheConfig_PreserveOnSmallTurns_Valid(t *testing.T) {
	cache := &CacheConfig{
		Policy:            CachePolicyPreserveOnSmallTurns,
		SmallTurnThreshold: 100,
	}

	err := validateCacheConfig(cache)
	assert.NoError(t, err)
}

func TestValidateCacheConfig_FlushUnderPressure_InvalidThreshold(t *testing.T) {
	cache := &CacheConfig{
		Policy:                CachePolicyFlushUnderPressure,
		MemoryPressureThreshold: 1.5, // Invalid (> 1.0)
	}

	err := validateCacheConfig(cache)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "memory_pressure_threshold must be between 0.0 and 1.0")
}

func TestValidateCacheConfig_FlushUnderPressure_Valid(t *testing.T) {
	cache := &CacheConfig{
		Policy:                CachePolicyFlushUnderPressure,
		MemoryPressureThreshold: 0.5,
	}

	err := validateCacheConfig(cache)
	assert.NoError(t, err)
}

func TestValidateInferenceConfig_InvalidTemperature(t *testing.T) {
	inference := &InferenceConfig{
		Temperature: -1.0, // Invalid
	}

	err := validateInferenceConfig(inference)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "temperature must be between 0.0 and 2.0")
}

func TestValidateInferenceConfig_TemperatureTooHigh(t *testing.T) {
	inference := &InferenceConfig{
		Temperature: 3.0, // Invalid (> 2.0)
	}

	err := validateInferenceConfig(inference)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "temperature must be between 0.0 and 2.0")
}

func TestValidateInferenceConfig_ValidTemperature(t *testing.T) {
	inference := &InferenceConfig{
		Temperature: 1.0,
	}

	err := validateInferenceConfig(inference)
	assert.NoError(t, err)
}

func TestValidateInferenceConfig_InvalidTopP(t *testing.T) {
	inference := &InferenceConfig{
		TopP: -0.1, // Invalid
	}

	err := validateInferenceConfig(inference)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "top_p must be between 0.0 and 1.0")
}

func TestValidateInferenceConfig_TopPTooHigh(t *testing.T) {
	inference := &InferenceConfig{
		TopP: 1.5, // Invalid (> 1.0)
	}

	err := validateInferenceConfig(inference)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "top_p must be between 0.0 and 1.0")
}

func TestValidateInferenceConfig_ValidTopP(t *testing.T) {
	inference := &InferenceConfig{
		TopP: 0.9,
	}

	err := validateInferenceConfig(inference)
	assert.NoError(t, err)
}

func TestValidateInferenceConfig_InvalidMaxTokens(t *testing.T) {
	maxTokens := 0
	inference := &InferenceConfig{
		MaxTokens: &maxTokens, // Invalid (< 1)
	}

	err := validateInferenceConfig(inference)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_tokens must be at least 1")
}

func TestValidateInferenceConfig_ValidMaxTokens(t *testing.T) {
	maxTokens := 100
	inference := &InferenceConfig{
		MaxTokens: &maxTokens,
	}

	err := validateInferenceConfig(inference)
	assert.NoError(t, err)
}

func TestValidateInferenceConfig_NilMaxTokens(t *testing.T) {
	inference := &InferenceConfig{
		MaxTokens: nil, // Valid - nil means no limit
	}

	err := validateInferenceConfig(inference)
	assert.NoError(t, err)
}

func TestValidateAgenticServingConfig_CompleteValidConfig(t *testing.T) {
	maxTokens := 100
	cfg := &AgenticServingConfig{
		Mode: AgenticServingModeDaemon,
		Daemon: &DaemonConfig{
			ListenAddress: "localhost:8081",
		},
		LLMServers: []*LLMServerConfig{
			{
				Name:    "test-server",
				Backend: &core.LLMBackend{Type: core.LLMInferenceAPITypeOllama, Endpoint: "http://localhost:8080"},
				Model:   "test-model",
				Context: &ContextConfig{
					Shared:       true,
					SyncInterval: 50,
				},
				Cache: &CacheConfig{
					Policy:                CachePolicyFlushUnderPressure,
					MemoryPressureThreshold: 0.8,
				},
			},
		},
		AgentProfiles: []*AgentProfile{
			{
				Name:      "test-profile",
				LLMServer: "test-server",
				Inference: &InferenceConfig{
					Temperature: 0.7,
					TopP:        0.9,
					MaxTokens:   &maxTokens,
				},
				Tools: &ToolsConfig{
					Allowed: []string{"tool1", "tool2"},
				},
			},
		},
		Workflows: []*Workflow{
			{
				Name: "test-workflow",
				Tasks: []*WorkflowTask{
					{
						AgentProfile: "test-profile",
						LLMServer:    "test-server",
						Turn:         1,
						Prompt:       "test prompt",
						UseContext:   true,
					},
				},
			},
		},
	}

	err := validateAgenticServingConfig(cfg)
	require.NoError(t, err)
}
