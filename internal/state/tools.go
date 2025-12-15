package state

import "fmt"

// ToolNotFoundError is an error that is returned when a tool is not found
// in the tools registry.
type ToolNotFoundError struct {
	Name string `json:"name"`
}

// Error returns the error message for the ToolNotFoundError
func (e *ToolNotFoundError) Error() string {
	return fmt.Sprintf("tool not found: %s", e.Name)
}

// NewToolNotFoundError creates a new ToolNotFoundError
func NewToolNotFoundError(name string) *ToolNotFoundError {
	return &ToolNotFoundError{Name: name}
}

// Interface guard for ToolNotFoundError
// This ensures that ToolNotFoundError implements the error interface.
var _ error = &ToolNotFoundError{}

// ToolEntry represents a tool entry in the configuration
type ToolEntry struct {
	Name        string `yaml:"name"`        // the name of the tool
	Description string `yaml:"description"` // the description of the tool
	Path        string `yaml:"path"`        // the path to the tool
	Interpreter string `yaml:"interpreter"` // the interpreter to use for the tool
}

// ToolsRegistry maintains a registry of tools and their entries.
type ToolsRegistry struct {
	Tools map[string]*ToolEntry `yaml:"tools"` // the tools in the registry
}

// NewToolsRegistry creates a new tools registry
func NewToolsRegistry() *ToolsRegistry {
	return &ToolsRegistry{
		Tools: make(map[string]*ToolEntry),
	}
}

// NewToolsRegistryFromDirectory creates a new tools registry from a directory
func NewToolsRegistryFromDirectory(dir string) (*ToolsRegistry, error) {
	tools, err := ScanToolsFromDirectory(dir)
	if err != nil {
		return nil, err
	}
	return &ToolsRegistry{Tools: tools}, nil
}

// AddTool adds a tool to the registry
func (r *ToolsRegistry) AddTool(tool *ToolEntry) error {
	if _, ok := r.Tools[tool.Name]; ok {
		return NewDuplicateToolNameError(tool.Name)
	}
	r.Tools[tool.Name] = tool
	return nil
}

// GetTool returns a tool from the registry
func (r *ToolsRegistry) GetTool(name string) (*ToolEntry, error) {
	tool, ok := r.Tools[name]
	if !ok {
		return nil, NewToolNotFoundError(name)
	}
	return tool, nil
}

// ListTools returns all tools in the registry
func (r *ToolsRegistry) ListTools() []*ToolEntry {
	tools := make([]*ToolEntry, 0, len(r.Tools))
	for _, tool := range r.Tools {
		tools = append(tools, tool)
	}
	return tools
}
