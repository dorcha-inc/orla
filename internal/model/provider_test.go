package model

import (
	"testing"

	"github.com/harvard-cns/orla/internal/config"
	"github.com/harvard-cns/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseModelIdentifier(t *testing.T) {
	tests := []struct {
		name          string
		modelID       string
		expectedProv  string
		expectedModel string
		expectedErr   bool
	}{
		{
			name:          "valid openai model",
			modelID:       "openai:llama3",
			expectedProv:  "openai",
			expectedModel: "llama3",
			expectedErr:   false,
		},
		{
			name:          "valid model with version",
			modelID:       "openai:llama3:8b",
			expectedProv:  "openai",
			expectedModel: "llama3:8b",
			expectedErr:   false,
		},
		{
			name:          "missing colon",
			modelID:       "ollamallama3",
			expectedProv:  "",
			expectedModel: "",
			expectedErr:   true,
		},
		{
			name:          "empty string",
			modelID:       "",
			expectedProv:  "",
			expectedModel: "",
			expectedErr:   true,
		},
		{
			name:          "only provider",
			modelID:       "openai:",
			expectedProv:  "openai",
			expectedModel: "",
			expectedErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, model, err := ParseModelIdentifier(tt.modelID)
			if tt.expectedErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid model identifier format")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedProv, prov)
				assert.Equal(t, tt.expectedModel, model)
			}
		})
	}
}

func TestNewProvider(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.OrlaConfig
		expectedErr bool
		errContains string
	}{
		{
			name: "valid openai config",
			cfg: &config.OrlaConfig{
				Model:      "openai:llama3",
				LLMBackend: &core.LLMBackend{Endpoint: "http://localhost:11434/v1"},
			},
			expectedErr: false,
		},
		{
			name: "missing model",
			cfg: &config.OrlaConfig{
				Model: "",
			},
			expectedErr: true,
			errContains: "model not configured",
		},
		{
			name: "invalid model format",
			cfg: &config.OrlaConfig{
				Model: "invalid",
			},
			expectedErr: true,
			errContains: "invalid model identifier format",
		},
		{
			name: "unknown provider",
			cfg: &config.OrlaConfig{
				Model: "unknown:model",
			},
			expectedErr: true,
			errContains: "unknown model provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(tt.cfg)
			if tt.expectedErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, provider)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, "openai", provider.Name())
			}
		})
	}
}

func TestRegisterProviderFactory(t *testing.T) {
	RegisterProviderFactory("custom", func(modelName string, backend *core.LLMBackend, cfg *config.OrlaConfig) (Provider, error) {
		assert.Equal(t, "demo", modelName)
		assert.NotNil(t, cfg)
		_ = backend
		return NewMockProvider().WithName("stub").WithContent("ok").Build(), nil
	})

	cfg := &config.OrlaConfig{
		Model:      "custom:demo",
		LLMBackend: &core.LLMBackend{Endpoint: "http://example"},
	}
	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	assert.Equal(t, "stub", provider.Name())
}
