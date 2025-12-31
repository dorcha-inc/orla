package model

import (
	"testing"

	"github.com/dorcha-inc/orla/internal/config"
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
			name:          "valid ollama model",
			modelID:       "ollama:llama3",
			expectedProv:  "ollama",
			expectedModel: "llama3",
			expectedErr:   false,
		},
		{
			name:          "valid model with version",
			modelID:       "ollama:llama3:8b",
			expectedProv:  "ollama",
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
			modelID:       "ollama:",
			expectedProv:  "ollama",
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
			name: "valid ollama config",
			cfg: &config.OrlaConfig{
				Model: "ollama:llama3",
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
				assert.Equal(t, "ollama", provider.Name())
			}
		})
	}
}
