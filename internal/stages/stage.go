// Package stages owns the platform-engineer-facing stage record and the
// registry that persists it.
//
// A stage is two things: a routing label (which backend serves calls
// for this stage) and an observability label (the dimension we group
// telemetry by). The developer's only contract is "tag every call with
// a stage id." The platform engineer fills in the mapping later.
package stages

import "time"

// Stage is the persistent record for a single stage id. The zero
// values for Backend, ReasoningEffort, Prompt, and Labels are the "not
// yet configured" state, auto-created on first sighting by the proxy.
type Stage struct {
	ID              string         `json:"id"`
	Backend         string         `json:"backend"`
	ReasoningEffort string         `json:"reasoning_effort"`
	Prompt          string         `json:"prompt"`
	CaptureIO       bool           `json:"capture_io"`
	Labels          map[string]any `json:"labels"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// PatchRequest describes a partial update to a Stage. Nil pointers and
// nil maps mean "leave this field untouched." Non-nil maps replace the
// stored labels entirely, merge-style updates are intentionally not
// supported in v1 to keep the wire contract simple.
type PatchRequest struct {
	Backend         *string        `json:"backend,omitempty"`
	ReasoningEffort *string        `json:"reasoning_effort,omitempty"`
	Prompt          *string        `json:"prompt,omitempty"`
	CaptureIO       *bool          `json:"capture_io,omitempty"`
	Labels          map[string]any `json:"labels,omitempty"`
}
