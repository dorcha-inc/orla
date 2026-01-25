// Package model provides cache control for LLM backends (RFC 5).
package model

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"go.uber.org/zap"
)

// CacheController is the interface for backend-specific cache control
type CacheController interface {
	// FlushCache flushes the KV cache for the backend
	FlushCache(ctx context.Context) error
	// GetCacheState returns the current cache state
	GetCacheState() CacheState
}

// CacheState represents the state of a cache
type CacheState struct {
	// IsFlushed indicates whether the cache has been flushed
	IsFlushed bool
	// LastFlushTime is the timestamp of the last flush
	LastFlushTime int64
}

// SGLangCacheController implements cache control for SGLang backends
type SGLangCacheController struct {
	// baseURL is the base URL of the SGLang server
	baseURL string
	// client is the HTTP client for making requests
	client *http.Client
	// state tracks the cache state
	state CacheState
	// mu protects access to state
	mu sync.RWMutex
}

// NewSGLangCacheController creates a new SGLang cache controller
func NewSGLangCacheController(baseURL string, client *http.Client) *SGLangCacheController {
	if client == nil {
		client = http.DefaultClient
	}

	return &SGLangCacheController{
		baseURL: baseURL,
		client:  client,
		state: CacheState{
			IsFlushed: false,
		},
	}
}

// FlushCache flushes the KV cache by calling SGLang's /flush_cache endpoint
func (c *SGLangCacheController) FlushCache(ctx context.Context) error {
	// Construct the flush cache endpoint URL
	flushURL := fmt.Sprintf("%s/flush_cache", c.baseURL)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", flushURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create flush cache request: %w", err)
	}

	// Send the request
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to flush cache: %w", err)
	}
	core.LogDeferredError(resp.Body.Close)

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("flush cache returned status %d", resp.StatusCode)
	}

	// Update cache state
	c.mu.Lock()
	c.state.IsFlushed = true
	c.mu.Unlock()

	zap.L().Debug("Flushed SGLang cache",
		zap.String("base_url", c.baseURL))

	return nil
}

// GetCacheState returns the current cache state
func (c *SGLangCacheController) GetCacheState() CacheState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// NewCacheController creates a cache controller based on the LLM server configuration
func NewCacheController(serverConfig *config.LLMServerConfig) (CacheController, error) {
	if serverConfig == nil || serverConfig.Backend == nil {
		return nil, fmt.Errorf("server config and backend are required")
	}

	switch serverConfig.Backend.Type {
	case core.LLMInferenceAPITypeSGLang:
		// SGLang has a dedicated /flush_cache endpoint
		if serverConfig.Backend.Endpoint == "" {
			return nil, fmt.Errorf("endpoint is required for SGLang backend")
		}
		// Normalize endpoint (remove trailing slash)
		endpoint := strings.TrimSuffix(serverConfig.Backend.Endpoint, "/")
		// Remove /v1 suffix if present (SGLang flush endpoint is at base URL)
		endpoint = strings.TrimSuffix(endpoint, "/v1")
		return NewSGLangCacheController(endpoint, nil), nil

	case core.LLMInferenceAPITypeOpenAI:
		// OpenAI-compatible backends may not support cache control
		return nil, fmt.Errorf("cache control not supported for OpenAI-compatible backend")

	case core.LLMInferenceAPITypeOllama:
		// Ollama uses keep_alive parameter, not a flush endpoint
		// Could implement OllamaCacheController that sets keep_alive=0 in requests
		// For now, return nil to indicate cache control not yet implemented
		return nil, fmt.Errorf("ollama cache control via keep_alive not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported backend type for cache control: %s", serverConfig.Backend.Type)
	}
}
