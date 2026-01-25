package model

import (
	"fmt"
	"strings"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
)

// ParseModelIdentifier parses a model identifier string (e.g., "ollama:llama3")
// and returns the provider name and model name
func ParseModelIdentifier(modelID string) (provider, modelName string, err error) {
	parts := strings.SplitN(modelID, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model identifier format: expected 'provider:model-name', got '%s'", modelID)
	}
	return parts[0], parts[1], nil
}

// NewProvider creates a new model provider based on the configuration
func NewProvider(cfg *config.OrlaConfig) (Provider, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("model not configured")
	}

	providerName, modelName, err := ParseModelIdentifier(cfg.Model)
	if err != nil {
		return nil, err
	}

	supportedProviders := map[core.LLMInferenceAPIType]struct{}{
		core.LLMInferenceAPITypeOllama: {},
		core.LLMInferenceAPITypeOpenAI: {},
		core.LLMInferenceAPITypeSGLang: {},
	}

	switch providerName {
	case string(core.LLMInferenceAPITypeOllama):
		return NewOllamaProvider(modelName, cfg)
	case string(core.LLMInferenceAPITypeSGLang):
		// SGLang is Ollama-compatible for inference, so use Ollama provider
		// If backend type is not explicitly set, default to Ollama-compatible
		if cfg.LLMBackend == nil {
			cfg.LLMBackend = &core.LLMBackend{
				Type: core.LLMInferenceAPITypeOllama,
			}
		} else if cfg.LLMBackend.Type == "" {
			cfg.LLMBackend.Type = core.LLMInferenceAPITypeOllama
		}

		if cfg.LLMBackend.Type != core.LLMInferenceAPITypeOllama {
			return nil, fmt.Errorf("for an SGLang backend, the Inference API type must be %s, got %s", core.LLMInferenceAPITypeOllama, cfg.LLMBackend.Type)
		}

		// Use Ollama provider for inference (SGLang is Ollama-compatible)
		return NewOllamaProvider(modelName, cfg)
	case string(core.LLMInferenceAPITypeOpenAI):
		// OpenAI-compatible provider (works with OpenAI, vLLM, SGLang, etc.)
		return NewOpenAIProvider(modelName, cfg)
	default:
		return nil, fmt.Errorf("unknown model provider: %s: supported providers are %s", providerName, core.JoinMapKeys(supportedProviders))
	}
}

// NewProviderFromLLMServerConfig creates a new model provider from an LLM server configuration (RFC 5)
func NewProviderFromLLMServerConfig(serverConfig *config.LLMServerConfig) (Provider, error) {
	if serverConfig == nil {
		return nil, fmt.Errorf("llm server configuration is required")
	}

	if serverConfig.Model == "" {
		return nil, fmt.Errorf("model is required in llm server configuration")
	}

	// SGLang is Ollama-compatible for inference, so use Ollama provider
	// The backend type "sglang" is used for cache control, but inference uses Ollama-compatible API
	backendType := serverConfig.Backend.Type
	if backendType == core.LLMInferenceAPITypeSGLang {
		// Create a temporary backend config with Ollama type for provider creation
		// The original SGLang type is preserved in serverConfig.Backend for cache control
		ollamaBackend := &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeOllama,
			Endpoint: serverConfig.Backend.Endpoint,
			// SGLang via Ollama-compatible API doesn't require API key
		}
		cfg := &config.OrlaConfig{
			LLMBackend: ollamaBackend,
			Model:      serverConfig.Model,
		}
		return NewProvider(cfg)
	}

	// Create a temporary OrlaConfig to use existing provider creation logic
	// This is a bridge until we refactor providers to accept LLMServerConfig directly
	cfg := &config.OrlaConfig{
		LLMBackend: serverConfig.Backend,
		Model:      serverConfig.Model,
	}

	return NewProvider(cfg)
}
