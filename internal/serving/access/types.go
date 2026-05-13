// Package access implements access control policy evaluation for the Orla serving layer.
//
// Policies follow a deny-overrides model. This means that
// any matching "deny" rule wins over any matching "allow" rules.
// If no policies match, access is allowed by default.
//
// Subjects and resources support glob patterns (e.g., "tenant:*", "backend:gpt*").
package access

// Action is the effect of an access control policy.
type Action string

const (
	// ActionAllow permits the subject to access the resource.
	ActionAllow Action = "allow"
	// ActionDeny forbids the subject from accessing the resource.
	ActionDeny Action = "deny"
)

// ResourceType categorizes what a policy governs.
type ResourceType string

const (
	ResourceTypeBackend ResourceType = "backend"
	ResourceTypeTool    ResourceType = "tool"
	ResourceTypeData    ResourceType = "data"
)

// Policy is a single access control rule.
type Policy struct {
	// Name uniquely identifies this policy.
	Name string `json:"name"`
	// Subjects are glob patterns matching request tags (e.g., "tenant:acme", "workflow:prod-*").
	Subjects []string `json:"subjects"`
	// Resources are glob patterns matching resource identifiers
	// (e.g., "backend:gpt4o", "tool:shell_*", "data:pii").
	Resources []string `json:"resources"`
	// Action is "allow" or "deny".
	Action Action `json:"action"`
}
