package tool

import (
	"fmt"
	"io"
	"os"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/dorcha-inc/orla/internal/registry"
)

// InstallOptions configures tool installation
type InstallOptions struct {
	RegistryURL string
	Version     string
	LocalPath   string
	Writer      io.Writer
}

// InstallTool installs a tool from the registry or local path
func InstallTool(toolName string, opts InstallOptions) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	// Handle local installation
	if opts.LocalPath != "" {
		if err := installer.InstallLocalTool(opts.LocalPath, opts.Writer); err != nil {
			return fmt.Errorf("failed to install local tool: %w", err)
		}
		core.MustFprintf(opts.Writer, "Tool is now available. Restart orla server to use it.\n")
		return nil
	}

	// Use default registry if not specified
	if opts.RegistryURL == "" {
		opts.RegistryURL = registry.DefaultRegistryURL
	}

	// Use "latest" if version not specified
	if opts.Version == "" {
		opts.Version = "latest"
	}

	// Install the tool
	if err := installer.InstallTool(opts.RegistryURL, toolName, opts.Version, opts.Writer); err != nil {
		return fmt.Errorf("failed to install tool: %w", err)
	}

	core.MustFprintf(opts.Writer, "Successfully installed %s\n", toolName)
	core.MustFprintf(opts.Writer, "Tool is now available. Restart orla server to use it.\n")

	return nil
}
