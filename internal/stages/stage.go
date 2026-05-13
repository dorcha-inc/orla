// Package stages is the registry of platform-engineer-owned stage
// configuration. Developers tag LLM calls with a stage_id; the platform
// engineer (or an external mapper) writes stage records that determine
// backend selection, scheduling, cache behavior, and reasoning effort.
package stages

import (
	"time"
)

// Stage is the configuration record for a named stage. Most fields are
// pointers so the wire shape can distinguish "unset" from "zero".
type Stage struct {
	ID string `json:"id"`

	// Backend names the LLM backend this stage routes to. Empty means
	// "unmapped" — the proxy falls back to the request's model field (and
	// will eventually return 400 once that fallback is removed).
	Backend string `json:"backend,omitempty"`

	// Scheduling policy applied when this stage's requests are queued.
	SchedulingPolicy        string `json:"scheduling_policy,omitempty"`
	RequestSchedulingPolicy string `json:"request_scheduling_policy,omitempty"`
	Priority                *int   `json:"priority,omitempty"`
	RequestPriority         *int   `json:"request_priority,omitempty"`
	MaxConcurrency          *int   `json:"max_concurrency,omitempty"`

	// ReasoningEffort is the value forwarded to the model (platform-engineer-owned).
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// FlushOnComplete asks the memory manager to drop this stage's KV cache
	// region when its workflow run completes.
	FlushOnComplete bool `json:"flush_on_complete,omitempty"`

	// ReasoningHint is a developer-supplied hint (e.g. "high"/"medium"/"low").
	// Recorded for the platform engineer's mapper to read; does not affect
	// inference directly.
	ReasoningHint string `json:"reasoning_hint,omitempty"`

	// Labels is free-form metadata for the platform engineer.
	Labels map[string]string `json:"labels,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// clone returns a deep copy of s. Used by the cache so callers can't mutate
// the cached pointer.
func (s *Stage) clone() *Stage {
	if s == nil {
		return nil
	}
	out := *s
	if s.Priority != nil {
		v := *s.Priority
		out.Priority = &v
	}
	if s.RequestPriority != nil {
		v := *s.RequestPriority
		out.RequestPriority = &v
	}
	if s.MaxConcurrency != nil {
		v := *s.MaxConcurrency
		out.MaxConcurrency = &v
	}
	if len(s.Labels) > 0 {
		out.Labels = make(map[string]string, len(s.Labels))
		for k, v := range s.Labels {
			out.Labels[k] = v
		}
	}
	return &out
}
