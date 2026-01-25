// Package api provides the HTTP client for communicating with the Agentic Serving Layer daemon (RFC 5).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dorcha-inc/orla/internal/core"
)

// Client is the HTTP client for communicating with the daemon
type Client struct {
	// baseURL is the base URL of the daemon
	baseURL string
	// httpClient is the HTTP client
	httpClient *http.Client
}

// NewClient creates a new daemon client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Health checks the health of the daemon
func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/health", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check health: %w", err)
	}
	defer core.LogDeferredError(resp.Body.Close)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// StartWorkflow starts a workflow execution
func (c *Client) StartWorkflow(ctx context.Context, workflowName string) (*WorkflowStartResponse, error) {
	url := fmt.Sprintf("%s/api/v1/workflow/start", c.baseURL)

	req := WorkflowStartRequest{
		WorkflowName: workflowName,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow start request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to start workflow: %w", err)
	}
	defer core.LogDeferredError(httpResp.Body.Close)

	if httpResp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, fmt.Errorf("workflow start returned status %d: failed to read body: %w", httpResp.StatusCode, err)
		}
		return nil, fmt.Errorf("workflow start returned status %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var workflowResp WorkflowStartResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&workflowResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &workflowResp, nil
}
