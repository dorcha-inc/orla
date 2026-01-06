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
	}

	switch providerName {
	case string(core.LLMInferenceAPITypeOllama):
		return NewOllamaProvider(modelName, cfg)
	default:
		return nil, fmt.Errorf("unknown model provider: %s: supported providers are %s", providerName, core.JoinMapKeys(supportedProviders))
	}
}
