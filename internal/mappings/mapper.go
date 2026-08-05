package mappings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// maxDecisionBytes bounds a stage mapper response body. A decision
// is one backend name, anything near this limit is a misconfigured
// URL.
const maxDecisionBytes = 1 << 16

// Candidate is one backend a stage mapper may route a stage to. The
// costs are the per-million-token prices Orla will bill with, live
// polled values when held and the static columns otherwise. Nil means
// the backend has no configured cost.
type Candidate struct {
	Name                string   `json:"name"`
	Quality             *float64 `json:"quality,omitempty"`
	InputCostPerMtoken  *float64 `json:"input_cost_per_mtoken,omitempty"`
	OutputCostPerMtoken *float64 `json:"output_cost_per_mtoken,omitempty"`
	QueueDepth          int64    `json:"queue_depth"`
	InFlight            int64    `json:"in_flight"`
	Capacity            int      `json:"capacity"`
	Circuit             string   `json:"circuit"`
}

// DecideRequest is what the proxy POSTs to the stage mapper for one
// routing decision. Current is the stage's static backend, empty when
// the stage has none.
type DecideRequest struct {
	Stage      string            `json:"stage"`
	Tags       map[string]string `json:"tags,omitempty"`
	Current    string            `json:"current"`
	Candidates []Candidate       `json:"candidates"`
}

// decideResponse is the mapper's answer. An empty backend means the
// mapper declines and the stage routes by its static mapping.
type decideResponse struct {
	Backend string `json:"backend"`
}

// HTTPMapper asks an external service which backend should serve a
// stage. Construct with NewHTTPMapper.
type HTTPMapper struct {
	url    string
	client *http.Client
}

// NewHTTPMapper returns a mapper that calls url for each decision.
// timeout bounds a single decision. The caller validates url before
// constructing the mapper.
func NewHTTPMapper(url string, timeout time.Duration) *HTTPMapper {
	return &HTTPMapper{
		url:    url,
		client: &http.Client{Timeout: timeout},
	}
}

// Decide returns the backend the mapper chose, or "" when the mapper
// declines. The caller validates the choice against the candidates it
// offered.
func (p *HTTPMapper) Decide(ctx context.Context, req DecideRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal decide request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build decide request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("call stage mapper: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stage mapper returned status %d", resp.StatusCode)
	}

	var decision decideResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDecisionBytes)).Decode(&decision); err != nil {
		return "", fmt.Errorf("decode decide response: %w", err)
	}
	return decision.Backend, nil
}

// MapperHolder is the mutable slot the proxy reads its stage mapper
// from. The control plane swaps the mapper at runtime while requests
// are in flight. The zero value holds no mapper. Safe for concurrent
// use.
type MapperHolder struct {
	mu     sync.RWMutex
	mapper *HTTPMapper
}

// Set installs mapper, replacing any prior one. A nil mapper disables
// dynamic routing.
func (h *MapperHolder) Set(mapper *HTTPMapper) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mapper = mapper
}

// Get returns the active mapper, or nil when dynamic routing is
// disabled.
func (h *MapperHolder) Get() *HTTPMapper {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.mapper
}
