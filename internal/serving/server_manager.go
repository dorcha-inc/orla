// Package serving implements the Agentic Serving Layer (RFC 5).
package serving

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/model"
	"go.uber.org/zap"
)

// LLMServerManager manages a pool of LLM server configurations and their providers
type LLMServerManager struct {
	// servers maps server names to their configurations
	servers map[string]*config.LLMServerConfig
	// providers maps server names to their cached providers
	providers map[string]model.Provider
	// cacheControllers maps server names to their cache controllers
	cacheControllers map[string]model.CacheController
	// mu protects access to servers, providers, and cacheControllers
	mu sync.RWMutex
}

// NewLLMServerManager creates a new LLM server manager
func NewLLMServerManager(servers []*config.LLMServerConfig) *LLMServerManager {
	serverMap := make(map[string]*config.LLMServerConfig)
	for _, server := range servers {
		serverMap[server.Name] = server
	}

	return &LLMServerManager{
		servers:          serverMap,
		providers:        make(map[string]model.Provider),
		cacheControllers: make(map[string]model.CacheController),
	}
}

// GetServerConfig returns the LLM server configuration for a given name
func (m *LLMServerManager) GetServerConfig(name string) (*config.LLMServerConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	server, exists := m.servers[name]
	if !exists {
		return nil, fmt.Errorf("llm_server '%s' not found", name)
	}
	return server, nil
}

// GetProvider returns a cached provider for an LLM server, creating it if necessary
func (m *LLMServerManager) GetProvider(ctx context.Context, serverName string) (model.Provider, error) {
	// Check cache first
	m.mu.RLock()
	if provider, exists := m.providers[serverName]; exists {
		m.mu.RUnlock()
		return provider, nil
	}
	m.mu.RUnlock()

	// Get server config
	server, err := m.GetServerConfig(serverName)
	if err != nil {
		return nil, err
	}

	// Create provider from server config
	provider, err := model.NewProviderFromLLMServerConfig(server)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider for llm_server '%s': %w", serverName, err)
	}

	// Cache the provider
	m.mu.Lock()
	m.providers[serverName] = provider
	m.mu.Unlock()

	zap.L().Debug("Created and cached provider for LLM server",
		zap.String("server_name", serverName),
		zap.String("model", server.Model))

	return provider, nil
}

// HealthStatus represents the health status of an LLM server
type HealthStatus string

const (
	// HealthStatusHealthy indicates the server is healthy
	HealthStatusHealthy HealthStatus = "healthy"
	// HealthStatusDegraded indicates the server is degraded
	HealthStatusDegraded HealthStatus = "degraded"
	// HealthStatusUnavailable indicates the server is unavailable
	HealthStatusUnavailable HealthStatus = "unavailable"
)

const (
	// healthCheckTimeout is the maximum time allowed for a health check
	healthCheckTimeout = 5 * time.Second
	// healthCheckDegradedThreshold is the response time threshold for degraded status
	// If health check takes longer than this, server is considered degraded
	healthCheckDegradedThreshold = 2 * time.Second
)

// GetHealthStatus returns the health status of an LLM server
// It performs actual health checks by calling the provider's EnsureReady method
// and measuring response times to determine health status
func (m *LLMServerManager) GetHealthStatus(ctx context.Context, serverName string) (HealthStatus, error) {
	// Check if server config exists
	_, err := m.GetServerConfig(serverName)
	if err != nil {
		return HealthStatusUnavailable, err
	}

	// Get the provider for this server
	provider, err := m.GetProvider(ctx, serverName)
	if err != nil {
		zap.L().Debug("Failed to get provider for health check",
			zap.String("server_name", serverName),
			zap.Error(err))
		return HealthStatusUnavailable, fmt.Errorf("failed to get provider: %w", err)
	}

	// Create a timeout context for the health check
	healthCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// Measure response time
	start := time.Now()
	err = provider.EnsureReady(healthCtx)
	duration := time.Since(start)

	// Check for timeout
	if healthCtx.Err() == context.DeadlineExceeded {
		zap.L().Warn("Health check timed out",
			zap.String("server_name", serverName),
			zap.Duration("timeout", healthCheckTimeout))
		return HealthStatusUnavailable, fmt.Errorf("health check timed out after %v", healthCheckTimeout)
	}

	// Check for errors
	if err != nil {
		zap.L().Warn("Health check failed",
			zap.String("server_name", serverName),
			zap.Duration("duration", duration),
			zap.Error(err))
		return HealthStatusUnavailable, fmt.Errorf("health check failed: %w", err)
	}

	// Determine status based on response time
	if duration > healthCheckDegradedThreshold {
		zap.L().Debug("Health check succeeded but response time indicates degraded status",
			zap.String("server_name", serverName),
			zap.Duration("duration", duration),
			zap.Duration("threshold", healthCheckDegradedThreshold))
		return HealthStatusDegraded, nil
	}

	zap.L().Debug("Health check succeeded",
		zap.String("server_name", serverName),
		zap.Duration("duration", duration))
	return HealthStatusHealthy, nil
}

// ListServers returns a list of all server names
func (m *LLMServerManager) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// GetCacheController returns a cached cache controller for an LLM server, creating it if necessary
func (m *LLMServerManager) GetCacheController(serverName string) (model.CacheController, error) {
	// Check cache first
	m.mu.RLock()
	if controller, exists := m.cacheControllers[serverName]; exists {
		m.mu.RUnlock()
		return controller, nil
	}
	m.mu.RUnlock()

	// Get server config
	server, err := m.GetServerConfig(serverName)
	if err != nil {
		return nil, err
	}

	// Create controller based on backend type
	controller, err := model.NewCacheController(server)
	if err != nil {
		// Cache control not supported for this backend - return nil, nil to indicate "not available"
		// This is not an error condition, just means this backend doesn't support cache control
		zap.L().Debug("Cache controller not available for server",
			zap.String("server_name", serverName),
			zap.Error(err))
		return nil, nil
	}

	// Cache the controller
	m.mu.Lock()
	m.cacheControllers[serverName] = controller
	m.mu.Unlock()

	zap.L().Debug("Created and cached cache controller for LLM server",
		zap.String("server_name", serverName))

	return controller, nil
}
