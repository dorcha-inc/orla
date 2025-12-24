// Package core implements the core functionality for orla that is shared across all components.
package core

// ToolEntry represents a tool entry for execution
type ToolEntry struct {
	Name        string         `yaml:"name"`                   // the name of the tool
	Description string         `yaml:"description"`            // the description of the tool
	Path        string         `yaml:"path"`                   // the path to the tool
	Interpreter string         `yaml:"interpreter"`            // the interpreter to use for the tool
	InputSchema map[string]any `yaml:"input_schema,omitempty"` // JSON schema for tool input parameters
}
