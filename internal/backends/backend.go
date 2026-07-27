// Package backends owns the backend record and the registry that
// persists it. A backend record carries everything orla needs to
// route a request: endpoint, model id or tool kind, concurrency cap,
// rate limit, and cost and quality priors.
//
// Backends are explicitly registered by the platform engineer. The
// proxy refuses to dispatch to an unknown backend.
package backends

import "time"

// Kind discriminates between the two backend flavors.
//
//   - KindLLM is an OpenAI-compatible chat completion backend.
//     Cost comes from InputCostPerMtoken and OutputCostPerMtoken.
//     ModelID is required.
//
//   - KindTool is any non-LLM backend such as a structure-prediction
//     service, a docking engine, or an external HTTP API. Cost comes
//     from the generic Rates map, with the tool reporting matching
//     usage at dispatch time. ToolKind is required. ModelID is unused.
type Kind string

const (
	KindLLM  Kind = "llm"
	KindTool Kind = "tool"
)

// Backend is the persistent record for a single backend.
//
// Cost and quality are platform-engineer-supplied priors. orla does
// not act on them, the mapper does. They are persisted so the mapper
// can read them as part of its state.
type Backend struct {
	Name           string   `json:"name"`
	Endpoint       string   `json:"endpoint"`
	APIKeyEnvVar   string   `json:"api_key_env_var"`
	MaxConcurrency int32    `json:"max_concurrency"`
	Quality        *float64 `json:"quality,omitempty"`

	// RatePerSecond is the requests/second cap applied per orla
	// instance. Nil or 0 means no limit. Bursting is allowed up to the
	// rounded-up rate so a steady-state limit of 5 rps doesn't reject
	// a sudden 5-request batch.
	RatePerSecond *float64 `json:"rate_per_second,omitempty"`

	// Kind discriminates LLM vs tool. Defaults to KindLLM at registration
	// time if unset.
	Kind Kind `json:"kind"`

	// ModelID is the provider-prefixed model identifier
	// (e.g., "openai:gpt-4o"). Required for KindLLM, unused for KindTool.
	ModelID *string `json:"model_id,omitempty"`

	// LLM cost model: per-million-token rates. NULL for KindTool.
	InputCostPerMtoken  *float64 `json:"input_cost_per_mtoken,omitempty"`
	OutputCostPerMtoken *float64 `json:"output_cost_per_mtoken,omitempty"`

	// ToolKind identifies the family of tool, such as "structure-prediction"
	// or "docking". Required for KindTool, unused for KindLLM.
	ToolKind *string `json:"tool_kind,omitempty"`

	// Rates is the per-resource pricing map for KindTool backends.
	// Keys are resource names that tool responses report in their
	// Usage map. Values are USD-per-unit. Examples:
	//
	//   {"gpu_seconds": 0.001}
	//   {"cpu_seconds": 0.0001, "calls": 0.005}
	//
	// Orla computes cost as the dot product of usage and rates. If a
	// tool reports Usage for a key not present in Rates, that key
	// contributes zero and a warning is logged.
	//
	// Rates is only valid for KindTool. LLM backends price through
	// InputCostPerMtoken and OutputCostPerMtoken and the API rejects
	// registration of a KindLLM backend that supplies Rates.
	Rates map[string]float64 `json:"rates,omitempty"`

	// CostSource is an optional URL the daemon polls for the backend's
	// current per-million-token costs. While a fetched price is live it
	// overrides InputCostPerMtoken and OutputCostPerMtoken for cost
	// accounting. Only valid for KindLLM.
	CostSource *string `json:"cost_source,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PatchRequest describes a partial update. nil pointers leave the
// corresponding field unchanged. Name, Kind, ModelID, and ToolKind
// cannot be patched. To change them, delete and re-create the backend.
type PatchRequest struct {
	Endpoint            *string  `json:"endpoint,omitempty"`
	APIKeyEnvVar        *string  `json:"api_key_env_var,omitempty"`
	MaxConcurrency      *int32   `json:"max_concurrency,omitempty"`
	InputCostPerMtoken  *float64 `json:"input_cost_per_mtoken,omitempty"`
	OutputCostPerMtoken *float64 `json:"output_cost_per_mtoken,omitempty"`
	Quality             *float64 `json:"quality,omitempty"`
	RatePerSecond       *float64 `json:"rate_per_second,omitempty"`

	// Rates uses a pointer so the patch can distinguish three cases:
	// an absent field (no change), JSON null or pointer to nil (clear),
	// and a pointer to a populated map (overwrite). Bare maps cannot
	// represent the null-vs-absent split.
	Rates *map[string]float64 `json:"rates,omitempty"`

	// CostSource follows the same convention as the other pointer
	// fields. A pointer to the empty string clears the source and
	// returns the backend to its static costs.
	CostSource *string `json:"cost_source,omitempty"`
}
