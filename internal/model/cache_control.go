// Package model provides cache control for LLM backends (RFC 5).
package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// GetMemoryPressure returns the current memory pressure (0.0-1.0)
	// Returns 0.0 if memory pressure cannot be determined
	GetMemoryPressure(ctx context.Context) (float64, error)
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
	defer core.LogDeferredError(resp.Body.Close)

	// Read response body to check for "Cache flushed" message
	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		bodyBytes = nil
	}
	bodyStr := string(bodyBytes)

	// SGLang may return 400 or 500 when there are running requests
	// If the body contains "Cache flushed", treat as success (operation was queued)
	if strings.Contains(bodyStr, "Cache flushed") {
		// Success - update cache state
		c.mu.Lock()
		c.state.IsFlushed = true
		c.mu.Unlock()

		zap.L().Debug("Flushed SGLang cache (non-200 with success message)",
			zap.Int("status_code", resp.StatusCode),
			zap.String("base_url", c.baseURL))
		return nil
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Under concurrent load, SGLang may return 500 when flush cannot be performed
		// This is expected behavior - log as warning but don't fail
		if resp.StatusCode == http.StatusInternalServerError {
			zap.L().Warn("SGLang cache flush returned 500 (likely due to running requests), continuing without flush",
				zap.String("base_url", c.baseURL),
				zap.String("response", bodyStr))
			// Don't update state - cache may not have been flushed
			return nil
		}
		// For other non-200 status codes, still return error
		if bodyStr != "" {
			return fmt.Errorf("flush cache returned status %d: %s", resp.StatusCode, bodyStr)
		}
		return fmt.Errorf("flush cache returned status %d", resp.StatusCode)
	}

	// Success - update cache state
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

// sglangServerInfo represents the response from SGLang's /get_server_info endpoint
type sglangServerInfo struct {
	InternalStates []sglangInternalState `json:"internal_states"`
}

// sglangInternalState represents an internal state entry from SGLang
type sglangInternalState struct {
	MemoryUsage sglangMemoryUsage `json:"memory_usage"`
}

// sglangMemoryUsage represents memory usage information from SGLang
type sglangMemoryUsage struct {
	// Weight is the fraction of memory used by model weights
	Weight float64 `json:"weight"`
	// KVCache is the fraction of memory used by KV cache (0.0-1.0)
	KVCache float64 `json:"kvcache"`
	// TokenCapacity is the maximum number of tokens that can be stored
	TokenCapacity int `json:"token_capacity"`
	// Graph is the fraction of memory used by CUDA graphs
	Graph float64 `json:"graph"`
}

// GetMemoryPressure queries SGLang for current KV cache memory pressure
// Returns the KV cache utilization as a fraction (0.0-1.0)
func (c *SGLangCacheController) GetMemoryPressure(ctx context.Context) (float64, error) {
	// Use /server_info (newer) or /get_server_info (deprecated but still works)
	serverInfoURL := fmt.Sprintf("%s/get_server_info", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", serverInfoURL, nil)
	if err != nil {
		return 0.0, fmt.Errorf("failed to create server info request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		zap.L().Debug("Failed to get SGLang server info, returning 0 memory pressure",
			zap.Error(err))
		return 0.0, nil // Return 0 on error to avoid triggering unnecessary flushes
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		zap.L().Debug("SGLang server info returned non-OK status, returning 0 memory pressure",
			zap.Int("status", resp.StatusCode))
		return 0.0, nil
	}

	var serverInfo sglangServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&serverInfo); err != nil {
		zap.L().Debug("Failed to decode SGLang server info, returning 0 memory pressure",
			zap.Error(err))
		return 0.0, nil
	}

	// Get memory pressure from first internal state (typical for single-GPU setup)
	if len(serverInfo.InternalStates) > 0 {
		pressure := serverInfo.InternalStates[0].MemoryUsage.KVCache
		zap.L().Debug("Got SGLang memory pressure",
			zap.Float64("kv_cache_pressure", pressure))
		return pressure, nil
	}

	return 0.0, nil
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
