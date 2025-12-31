// Package tool provides tool management functionality for Orla.
package tool

import (
	"fmt"
	"io"
	"os"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/dorcha-inc/orla/internal/registry"
)

// UpdateOptions configures tool update functionality given a registry URL
type UpdateOptions struct {
	RegistryURL string
	Writer      io.Writer
}

// UpdateTool updates a tool to the latest version from the given registry URL
func UpdateTool(toolName string, opts UpdateOptions) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	// Use default registry if not specified
	if opts.RegistryURL == "" {
		opts.RegistryURL = registry.DefaultRegistryURL
	}

	// Update the tool
	if err := installer.UpdateTool(opts.RegistryURL, toolName, opts.Writer); err != nil {
		return fmt.Errorf("failed to update tool: %w", err)
	}

	core.MustFprintf(opts.Writer, "✓ Successfully updated %s to latest version\n", toolName)
	core.MustFprintf(opts.Writer, "Restart orla server to use the updated version.\n")
	return nil
}
