package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RequestInfo is the per-request metadata a caller attaches when it
// acquires a slot. The scheduler forwards it to the scheduling policy
// so an external service can order the queue by stage, model, or tags.
// It carries no identity. The executor assigns each waiter its own id.
type RequestInfo struct {
	Stage string
	Model string
	Tags  map[string]string
}

// PendingRequest is one waiter as seen by a SchedulingPolicy. ID is
// assigned by the executor and is unique within a single Next call.
type PendingRequest struct {
	ID         string
	Info       RequestInfo
	EnqueuedAt time.Time
}

// BackendView is the executor state a SchedulingPolicy sees alongside
// the pending set.
type BackendView struct {
	Name       string
	Capacity   int
	InFlight   int64
	QueueDepth int64
}

// SchedulingPolicy picks which pending request a backend admits next
// when a slot frees. Implementations must return the ID of one of the
// pending requests. Returning an unknown ID or an error makes the
// executor fall back to first-come-first-served, so a policy is only
// ever an ordering hint and never gates correctness.
type SchedulingPolicy interface {
	Next(ctx context.Context, backend BackendView, pending []PendingRequest) (string, error)
}

// PolicyMetrics records scheduling-policy decision outcomes. The
// scheduler package defines it so metrics.Metrics can satisfy it
// without the scheduler importing the metrics package. A nil
// PolicyMetrics is a no-op.
type PolicyMetrics interface {
	IncPolicyDecision(backend, outcome string)
	ObservePolicyDecision(backend string, seconds float64)
}

// HTTPPolicy delegates the ordering decision to an external service
// over HTTP. It POSTs the pending set to the configured URL and reads
// back the ID to admit next. Any transport error, timeout, or
// malformed response surfaces as an error, and the executor falls back
// to first-come-first-served.
type HTTPPolicy struct {
	url    string
	client *http.Client
}

// NewHTTPPolicy returns a policy that calls url for each decision.
// timeout bounds a single decision. The caller validates url before
// constructing the policy.
func NewHTTPPolicy(url string, timeout time.Duration) *HTTPPolicy {
	return &HTTPPolicy{
		url:    url,
		client: &http.Client{Timeout: timeout},
	}
}

type scheduleRequestBackend struct {
	Name       string `json:"name"`
	Capacity   int    `json:"capacity"`
	InFlight   int64  `json:"inflight"`
	QueueDepth int64  `json:"queue_depth"`
}

type scheduleRequestPending struct {
	ID         string            `json:"id"`
	Stage      string            `json:"stage"`
	Model      string            `json:"model"`
	Tags       map[string]string `json:"tags,omitempty"`
	EnqueuedAt string            `json:"enqueued_at"`
	WaitMS     int64             `json:"wait_ms"`
}

type scheduleRequest struct {
	Backend scheduleRequestBackend   `json:"backend"`
	Pending []scheduleRequestPending `json:"pending"`
}

// scheduleResponse accepts either a single next id or an ordered list.
// Next is preferred. When Next is empty the first element of Order is
// used, which lets a service return a full ranking without the
// executor having to consume more than one decision at a time.
type scheduleResponse struct {
	Next  string   `json:"next"`
	Order []string `json:"order"`
}

// Next implements SchedulingPolicy.
func (p *HTTPPolicy) Next(ctx context.Context, backend BackendView, pending []PendingRequest) (string, error) {
	body, err := json.Marshal(buildScheduleRequest(backend, pending))
	if err != nil {
		return "", fmt.Errorf("marshal schedule request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build schedule request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call scheduling service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("scheduling service returned %d", resp.StatusCode)
	}
	var decoded scheduleResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode schedule response: %w", err)
	}
	if decoded.Next != "" {
		return decoded.Next, nil
	}
	if len(decoded.Order) > 0 {
		return decoded.Order[0], nil
	}
	return "", fmt.Errorf("scheduling service returned no decision")
}

func buildScheduleRequest(backend BackendView, pending []PendingRequest) scheduleRequest {
	now := time.Now()
	out := scheduleRequest{
		// BackendView and scheduleRequestBackend share the same fields,
		// so a conversion carries them over and adds the JSON tags.
		Backend: scheduleRequestBackend(backend),
		Pending: make([]scheduleRequestPending, 0, len(pending)),
	}
	for _, p := range pending {
		out.Pending = append(out.Pending, scheduleRequestPending{
			ID:         p.ID,
			Stage:      p.Info.Stage,
			Model:      p.Info.Model,
			Tags:       p.Info.Tags,
			EnqueuedAt: p.EnqueuedAt.UTC().Format(time.RFC3339Nano),
			WaitMS:     now.Sub(p.EnqueuedAt).Milliseconds(),
		})
	}
	return out
}
