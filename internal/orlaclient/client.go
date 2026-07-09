// Package orlaclient is the HTTP client and command tree for orlactl, the
// orla control-plane CLI. It talks to the daemon over the same HTTP API
// agents use and depends only on the standard library, cobra, and
// internal/wire, so the orlactl binary never links the database driver or
// the server packages. A depguard rule enforces that.
package orlaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/harvard-cns/orla/internal/wire"
)

// Client talks to an orla daemon's control-plane API.
type Client struct {
	addr string
	http *http.Client
}

// New returns a Client pointed at addr, e.g. "http://localhost:8081".
func New(addr string) *Client {
	return &Client{
		addr: strings.TrimRight(addr, "/"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.addr+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// CreateBackend registers a backend.
func (c *Client) CreateBackend(ctx context.Context, req wire.CreateBackendRequest) (wire.Backend, error) {
	var b wire.Backend
	err := c.do(ctx, http.MethodPost, "/api/v1/backends", req, &b)
	return b, err
}

// ListBackends returns every registered backend.
func (c *Client) ListBackends(ctx context.Context) ([]wire.Backend, error) {
	var out struct {
		Backends []wire.Backend `json:"backends"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/backends", nil, &out)
	return out.Backends, err
}

// GetBackend returns one backend by name.
func (c *Client) GetBackend(ctx context.Context, name string) (wire.Backend, error) {
	var b wire.Backend
	err := c.do(ctx, http.MethodGet, "/api/v1/backends/"+url.PathEscape(name), nil, &b)
	return b, err
}

// DeleteBackend removes a backend.
func (c *Client) DeleteBackend(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/backends/"+url.PathEscape(name), nil, nil)
}

// MapStage points a stage at a backend, replacing the stage record.
func (c *Client) MapStage(ctx context.Context, id string, req wire.MapStageRequest) (wire.Stage, error) {
	var s wire.Stage
	err := c.do(ctx, http.MethodPut, "/api/v1/stages/"+url.PathEscape(id), req, &s)
	return s, err
}

// ListStages returns every stage and its mapping.
func (c *Client) ListStages(ctx context.Context) ([]wire.Stage, error) {
	var out struct {
		Stages []wire.Stage `json:"stages"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/stages", nil, &out)
	return out.Stages, err
}

// GetStage returns one stage by id.
func (c *Client) GetStage(ctx context.Context, id string) (wire.Stage, error) {
	var s wire.Stage
	err := c.do(ctx, http.MethodGet, "/api/v1/stages/"+url.PathEscape(id), nil, &s)
	return s, err
}

// DeleteStage removes a stage.
func (c *Client) DeleteStage(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/stages/"+url.PathEscape(id), nil, nil)
}

// PutMapping creates or replaces a mapping variant.
func (c *Client) PutMapping(ctx context.Context, req wire.PutMappingRequest) (wire.Variant, error) {
	var v wire.Variant
	err := c.do(ctx, http.MethodPost, "/api/v1/mappings", req, &v)
	return v, err
}

// ListMappings returns every mapping variant.
func (c *Client) ListMappings(ctx context.Context) ([]wire.Variant, error) {
	var out struct {
		Mappings []wire.Variant `json:"mappings"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/mappings", nil, &out)
	return out.Mappings, err
}

// GetMapping returns one mapping variant by name.
func (c *Client) GetMapping(ctx context.Context, name string) (wire.Variant, error) {
	var v wire.Variant
	err := c.do(ctx, http.MethodGet, "/api/v1/mappings/"+url.PathEscape(name), nil, &v)
	return v, err
}

// DeleteMapping removes a mapping variant.
func (c *Client) DeleteMapping(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/mappings/"+url.PathEscape(name), nil, nil)
}

// GetSchedulerPolicy returns the active scheduling policy.
func (c *Client) GetSchedulerPolicy(ctx context.Context) (wire.SchedulerPolicy, error) {
	var p wire.SchedulerPolicy
	err := c.do(ctx, http.MethodGet, "/api/v1/scheduler/policy", nil, &p)
	return p, err
}

// SetSchedulerPolicy replaces the active scheduling policy. An empty URL
// reverts to first-come-first-served.
func (c *Client) SetSchedulerPolicy(ctx context.Context, req wire.SchedulerPolicy) (wire.SchedulerPolicy, error) {
	var p wire.SchedulerPolicy
	err := c.do(ctx, http.MethodPut, "/api/v1/scheduler/policy", req, &p)
	return p, err
}

// SubmitFeedback reports the outcome of a completion for a stage. Agents
// normally post this themselves; the CLI command is for trying the loop
// by hand.
func (c *Client) SubmitFeedback(ctx context.Context, req wire.FeedbackRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/feedback", req, nil)
}
