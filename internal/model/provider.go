package model

import (
	"fmt"
	"strings"
	"sync"

	"github.com/harvard-cns/orla/internal/config"
	"github.com/harvard-cns/orla/internal/core"
)

// ProviderFactory creates a provider for a parsed model name and backend context.
type ProviderFactory func(modelName string, backend *core.LLMBackend, cfg *config.OrlaConfig) (Provider, error)

var (
	providerRegistryMu sync.RWMutex
	providerRegistry   = map[string]ProviderFactory{}
)

// RegisterProviderFactory registers a provider factory by provider name (e.g. "openai").
func RegisterProviderFactory(providerName string, factory ProviderFactory) {
	providerRegistryMu.Lock()
	defer providerRegistryMu.Unlock()
	providerRegistry[strings.ToLower(providerName)] = factory
}

func getProviderFactory(providerName string) (ProviderFactory, bool) {
	providerRegistryMu.RLock()
	defer providerRegistryMu.RUnlock()
	factory, ok := providerRegistry[strings.ToLower(providerName)]
	return factory, ok
}

// ParseModelIdentifier parses a model identifier string (e.g., "openai:llama3")
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

	return newProviderForModel(cfg.Model, cfg.LLMBackend, cfg)
}

// NewProviderFromBackend creates a new model provider from a backend and model identifier.
// This is the programmatic entry point used by the serving layer.
func NewProviderFromBackend(backend *core.LLMBackend, modelID string) (Provider, error) {
	if modelID == "" {
		return nil, fmt.Errorf("model identifier is required")
	}

	cfg := &config.OrlaConfig{
		LLMBackend: backend,
		Model:      modelID,
	}

	return newProviderForModel(modelID, backend, cfg)
}

func newProviderForModel(modelID string, backend *core.LLMBackend, cfg *config.OrlaConfig) (Provider, error) {
	providerName, modelName, err := ParseModelIdentifier(modelID)
	if err != nil {
		return nil, err
	}

	factory, ok := getProviderFactory(providerName)
	if !ok {
		supportedProviders := map[string]struct{}{}
		providerRegistryMu.RLock()
		for provider := range providerRegistry {
			supportedProviders[provider] = struct{}{}
		}
		providerRegistryMu.RUnlock()
		return nil, fmt.Errorf("unknown model provider: %s: supported providers are %s", providerName, core.JoinMapKeys(supportedProviders))
	}
	return factory(modelName, backend, cfg)
}

func init() {
	// SGLang exposes the same OpenAI-compatible Chat Completions surface, so
	// both prefixes route to the OpenAI provider. The SGLang-specific cache
	// flush is wired separately by the backend manager when it sees a
	// "sglang:" prefix on the model identifier.
	openaiFactory := func(modelName string, _ *core.LLMBackend, cfg *config.OrlaConfig) (Provider, error) {
		return NewOpenAIProvider(modelName, cfg.LLMBackend)
	}
	RegisterProviderFactory("openai", openaiFactory)
	RegisterProviderFactory("sglang", openaiFactory)
}
