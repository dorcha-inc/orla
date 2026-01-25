package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSGLangCacheController(t *testing.T) {
	controller := NewSGLangCacheController("http://localhost:30000", nil)
	require.NotNil(t, controller)
	assert.Equal(t, "http://localhost:30000", controller.baseURL)
	assert.NotNil(t, controller.client)

	// Should use default client if nil
	controller2 := NewSGLangCacheController("http://localhost:30000", nil)
	assert.NotNil(t, controller2.client)
}

func TestNewSGLangCacheController_WithCustomClient(t *testing.T) {
	customClient := &http.Client{
		Timeout: 5 * time.Second,
	}
	controller := NewSGLangCacheController("http://localhost:30000", customClient)
	require.NotNil(t, controller)
	assert.Equal(t, customClient, controller.client)
}

func TestSGLangCacheController_FlushCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/flush_cache", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	controller := NewSGLangCacheController(server.URL, nil)
	ctx := context.Background()

	err := controller.FlushCache(ctx)
	require.NoError(t, err)

	// Check that state was updated
	state := controller.GetCacheState()
	assert.True(t, state.IsFlushed)
}

func TestSGLangCacheController_FlushCache_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	controller := NewSGLangCacheController(server.URL, nil)
	ctx := context.Background()

	err := controller.FlushCache(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")

	// State should not be updated on error
	state := controller.GetCacheState()
	assert.False(t, state.IsFlushed)
}

func TestSGLangCacheController_FlushCache_RequestError(t *testing.T) {
	// Use invalid URL to cause request error
	controller := NewSGLangCacheController("http://localhost:99999", nil)
	ctx := context.Background()

	err := controller.FlushCache(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to flush cache")
}

func TestSGLangCacheController_GetCacheState(t *testing.T) {
	controller := NewSGLangCacheController("http://localhost:30000", nil)

	// Initial state should be not flushed
	state := controller.GetCacheState()
	assert.False(t, state.IsFlushed)

	// After flush, state should be updated
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	controller.baseURL = server.URL
	ctx := context.Background()
	err := controller.FlushCache(ctx)
	require.NoError(t, err)

	state = controller.GetCacheState()
	assert.True(t, state.IsFlushed)
}

func TestNewCacheController_SGLang(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeSGLang,
			Endpoint: "http://localhost:30000",
		},
	}

	controller, err := NewCacheController(serverConfig)
	require.NoError(t, err)
	require.NotNil(t, controller)

	// Should be able to cast to SGLangCacheController
	sglangController, ok := controller.(*SGLangCacheController)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(sglangController.baseURL, "http://localhost:30000"))
}

func TestNewCacheController_SGLang_WithTrailingSlash(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeSGLang,
			Endpoint: "http://localhost:30000/",
		},
	}

	controller, err := NewCacheController(serverConfig)
	require.NoError(t, err)
	require.NotNil(t, controller)

	sglangController, ok := controller.(*SGLangCacheController)
	require.True(t, ok)
	// Trailing slash should be removed
	assert.Equal(t, "http://localhost:30000", sglangController.baseURL)
}

func TestNewCacheController_SGLang_WithV1Suffix(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeSGLang,
			Endpoint: "http://localhost:30000/v1",
		},
	}

	controller, err := NewCacheController(serverConfig)
	require.NoError(t, err)
	require.NotNil(t, controller)

	sglangController, ok := controller.(*SGLangCacheController)
	require.True(t, ok)
	// /v1 suffix should be removed
	assert.Equal(t, "http://localhost:30000", sglangController.baseURL)
}

func TestNewCacheController_SGLang_WithTrailingSlashAndV1(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeSGLang,
			Endpoint: "http://localhost:30000/v1/",
		},
	}

	controller, err := NewCacheController(serverConfig)
	require.NoError(t, err)
	require.NotNil(t, controller)

	sglangController, ok := controller.(*SGLangCacheController)
	require.True(t, ok)
	// Both trailing slash and /v1 should be removed
	assert.Equal(t, "http://localhost:30000", sglangController.baseURL)
}

func TestNewCacheController_SGLang_MissingEndpoint(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeSGLang,
			Endpoint: "",
		},
	}

	controller, err := NewCacheController(serverConfig)
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "endpoint is required")
}

func TestNewCacheController_OpenAI(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeOpenAI,
			Endpoint: "http://localhost:8000",
		},
	}

	controller, err := NewCacheController(serverConfig)
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "not supported")
}

func TestNewCacheController_Ollama(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     core.LLMInferenceAPITypeOllama,
			Endpoint: "http://localhost:11434",
		},
	}

	controller, err := NewCacheController(serverConfig)
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestNewCacheController_UnknownBackend(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: &core.LLMBackend{
			Type:     "unknown_backend",
			Endpoint: "http://localhost:8000",
		},
	}

	controller, err := NewCacheController(serverConfig)
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "unsupported backend type")
}

func TestNewCacheController_NilConfig(t *testing.T) {
	controller, err := NewCacheController(nil)
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "required")
}

func TestNewCacheController_NilBackend(t *testing.T) {
	serverConfig := &config.LLMServerConfig{
		Backend: nil,
	}

	controller, err := NewCacheController(serverConfig)
	assert.Error(t, err)
	assert.Nil(t, controller)
	assert.Contains(t, err.Error(), "required")
}
