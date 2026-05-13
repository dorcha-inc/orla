package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLLMBackend_Empty(t *testing.T) {
	backend := &LLMBackend{}
	assert.Empty(t, backend.Endpoint)
	assert.Empty(t, backend.APIKeyEnvVar)
}

func TestLLMBackend_WithValues(t *testing.T) {
	backend := &LLMBackend{
		Endpoint:     "http://localhost:8080",
		APIKeyEnvVar: "API_KEY",
	}

	assert.Equal(t, "http://localhost:8080", backend.Endpoint)
	assert.Equal(t, "API_KEY", backend.APIKeyEnvVar)
}
