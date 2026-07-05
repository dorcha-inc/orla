// Package mappings owns mapping variants and the registry that
// persists them.
//
// A mapping variant is a named, sparse set of per-stage backend
// overrides layered on the live stage mapping. The optimizer creates a
// variant to shadow-test a candidate, and a request selects it with the
// X-Orla-Mapping header. A stage absent from a variant resolves to the
// stage's live backend, so a candidate that differs from the live
// mapping on one stage is a single-entry variant.
package mappings

import "time"

// Variant is a named collection of per-stage backend overrides.
// Overrides maps a stage id to the backend that serves it under this
// variant. CreatedAt is the earliest override write, UpdatedAt the
// latest.
type Variant struct {
	Name      string            `json:"name"`
	Overrides map[string]string `json:"overrides"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}
