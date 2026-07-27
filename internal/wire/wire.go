// Package wire holds the JSON request and response types for orla's
// control-plane HTTP API. It is shared by the daemon's handlers and the
// orlactl client and depends only on the standard library, so the client
// binary never links the database driver or the server packages.
package wire

import "time"

// CreateBackendRequest is the POST /api/v1/backends body. Kind defaults
// to "llm" server-side when empty. ModelID is required for llm backends.
// ToolKind and Rates are required for tool backends.
type CreateBackendRequest struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind,omitempty"`
	Endpoint       string   `json:"endpoint"`
	APIKeyEnvVar   string   `json:"api_key_env_var"`
	MaxConcurrency int32    `json:"max_concurrency"`
	Quality        *float64 `json:"quality"`
	RatePerSecond  *float64 `json:"rate_per_second"`

	ModelID             string   `json:"model_id,omitempty"`
	InputCostPerMtoken  *float64 `json:"input_cost_per_mtoken,omitempty"`
	OutputCostPerMtoken *float64 `json:"output_cost_per_mtoken,omitempty"`
	CostSource          string   `json:"cost_source,omitempty"`

	ToolKind string             `json:"tool_kind,omitempty"`
	Rates    map[string]float64 `json:"rates,omitempty"`
}

// PatchBackendRequest is the PATCH /api/v1/backends/{name} body. Nil
// fields are left unchanged. Name, kind, model id, and tool kind cannot
// be patched.
type PatchBackendRequest struct {
	Endpoint            *string             `json:"endpoint,omitempty"`
	APIKeyEnvVar        *string             `json:"api_key_env_var,omitempty"`
	MaxConcurrency      *int32              `json:"max_concurrency,omitempty"`
	InputCostPerMtoken  *float64            `json:"input_cost_per_mtoken,omitempty"`
	OutputCostPerMtoken *float64            `json:"output_cost_per_mtoken,omitempty"`
	Quality             *float64            `json:"quality,omitempty"`
	RatePerSecond       *float64            `json:"rate_per_second,omitempty"`
	Rates               *map[string]float64 `json:"rates,omitempty"`
}

// Backend is the JSON the API returns for a backend. CircuitBreaker is
// live scheduler state, present on reads and empty otherwise.
type Backend struct {
	Name                string             `json:"name"`
	Endpoint            string             `json:"endpoint"`
	APIKeyEnvVar        string             `json:"api_key_env_var"`
	MaxConcurrency      int32              `json:"max_concurrency"`
	Quality             *float64           `json:"quality,omitempty"`
	RatePerSecond       *float64           `json:"rate_per_second,omitempty"`
	Kind                string             `json:"kind"`
	ModelID             *string            `json:"model_id,omitempty"`
	InputCostPerMtoken  *float64           `json:"input_cost_per_mtoken,omitempty"`
	OutputCostPerMtoken *float64           `json:"output_cost_per_mtoken,omitempty"`
	ToolKind            *string            `json:"tool_kind,omitempty"`
	Rates               map[string]float64 `json:"rates,omitempty"`
	CircuitBreaker      string             `json:"circuit_breaker,omitempty"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

// MapStageRequest is the PUT /api/v1/stages/{id} body. The PUT replaces
// the record, so omitted fields reset to their zero value.
type MapStageRequest struct {
	Backend         string         `json:"backend"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	Prompt          string         `json:"prompt,omitempty"`
	CaptureIO       bool           `json:"capture_io,omitempty"`
	Labels          map[string]any `json:"labels,omitempty"`
}

// PatchStageRequest is the PATCH /api/v1/stages/{id} body. Nil fields
// are left unchanged.
type PatchStageRequest struct {
	Backend         *string        `json:"backend,omitempty"`
	ReasoningEffort *string        `json:"reasoning_effort,omitempty"`
	Prompt          *string        `json:"prompt,omitempty"`
	CaptureIO       *bool          `json:"capture_io,omitempty"`
	Labels          map[string]any `json:"labels,omitempty"`
}

// PutMappingRequest is the POST /api/v1/mappings body. It creates or
// replaces a mapping variant named Name with the given per-stage
// backend overrides. Overrides must be non-empty, a variant with no
// overrides is indistinguishable from the live mapping.
type PutMappingRequest struct {
	Name      string            `json:"name"`
	Overrides map[string]string `json:"overrides"`
}

// Variant is the JSON the API returns for a mapping variant.
type Variant struct {
	Name      string            `json:"name"`
	Overrides map[string]string `json:"overrides"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Stage is the JSON the API returns for a stage.
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

// SchedulerPolicy is the JSON for GET and PUT /api/v1/scheduler/policy.
// An empty URL means first-come-first-served. TimeoutMS bounds a single
// scheduling decision. Enabled is derived on responses and ignored on
// input.
type SchedulerPolicy struct {
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeout_ms"`
	Enabled   bool   `json:"enabled"`
}

// CostPolicy is the GET and PUT /api/v1/costs/policy body.
// RefreshIntervalMS is how often the daemon refreshes prices from
// backend cost sources.
type CostPolicy struct {
	RefreshIntervalMS int `json:"refresh_interval_ms"`
}

// StageMapper is the GET and PUT /api/v1/stage-mapper body. An
// empty URL means stages route by their static mapping. TimeoutMS
// bounds a single routing decision. Enabled is derived on responses
// and ignored on input.
type StageMapper struct {
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeout_ms"`
	Enabled   bool   `json:"enabled"`
}

// FeedbackRequest is the POST /v1/feedback body. CompletionID and StageID
// are required. Rating, when set, must be between 0 and 1. Agents post
// this after a call so the mapper has an outcome signal.
type FeedbackRequest struct {
	CompletionID string   `json:"completion_id"`
	StageID      string   `json:"stage_id"`
	WorkflowRun  string   `json:"workflow_run,omitempty"`
	Rating       *float64 `json:"rating,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}
