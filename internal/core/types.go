// Package core implements the core functionality for orla that is shared across all components.
package core

// RuntimeMode represents the execution mode of a tool
type RuntimeMode string

const (
	// RuntimeModeSimple executes on-demand per request
	RuntimeModeSimple RuntimeMode = "simple"
	// RuntimeModeCapsule executes as a long-running process with lifecycle management
	RuntimeModeCapsule RuntimeMode = "capsule"
)

// HotLoadMode represents the reload strategy for hot-load
type HotLoadMode string

const (
	// HotLoadModeRestart restarts the tool when a watched file changes
	HotLoadModeRestart HotLoadMode = "restart"
)

// HotLoadConfig represents an RFC 3 compliant hot-reload configuration
// See RFC 3 section 5.3 for more details on the hot-load configuration
type HotLoadConfig struct {
	// Watch is a list of paths to watch for changes
	Watch []string `yaml:"watch,omitempty"`
	// Mode is the reload strategy. Currently only "restart" is supported
	Mode HotLoadMode `yaml:"mode,omitempty"`
	// DebounceMs is the minimum debounce interval for file change events in milliseconds
	DebounceMs int `yaml:"debounce_ms,omitempty"`
}

// RuntimeConfig represents RFC 3 compliant runtime configuration
type RuntimeConfig struct {
	// Mode is the runtime mode. Currently only "simple" and "capsule" are supported
	Mode RuntimeMode `yaml:"mode,omitempty"`
	// StartupTimeoutMs is the maximum time Orla will wait for the startup handshake in milliseconds
	StartupTimeoutMs int `yaml:"startup_timeout_ms,omitempty"`
	// HotLoad is the hot-reload configuration as defined in RFC 3 section 5.3
	HotLoad *HotLoadConfig `yaml:"hot_load,omitempty"`
	// Env is a map of environment variables to inject into the tool process
	Env map[string]string `yaml:"env,omitempty"`
	// Args is a list of command-line arguments to append to the entrypoint
	Args []string `yaml:"args,omitempty"`
}

// MCPConfig represents MCP-specific metadata from RFC 3
type MCPConfig struct {
	InputSchema  map[string]any `yaml:"input_schema,omitempty"`
	OutputSchema map[string]any `yaml:"output_schema,omitempty"`
}

// ToolManifest represents an RFC 3 compliant tool.yaml manifest
// It is used both for parsing manifests and for tool execution
type ToolManifest struct {
	Name         string         `yaml:"name" validate:"required"`
	Version      string         `yaml:"version" validate:"required"`
	Description  string         `yaml:"description" validate:"required"`
	Entrypoint   string         `yaml:"entrypoint" validate:"required"`
	Author       string         `yaml:"author,omitempty"`
	License      string         `yaml:"license,omitempty"`
	Repository   string         `yaml:"repository,omitempty"`
	Homepage     string         `yaml:"homepage,omitempty"`
	Keywords     []string       `yaml:"keywords,omitempty"`
	Dependencies []string       `yaml:"dependencies,omitempty"`
	MCP          *MCPConfig     `yaml:"mcp,omitempty"`
	Runtime      *RuntimeConfig `yaml:"runtime,omitempty"`
	Path         string         `yaml:"path,omitempty"`        // Absolute path to entrypoint
	Interpreter  string         `yaml:"interpreter,omitempty"` // Interpreter parsed from shebang
}
