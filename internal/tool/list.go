package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
)

// ListOptions configures the output format for listing tools
type ListOptions struct {
	JSON    bool
	Verbose bool
	Writer  io.Writer
}

// ListTools lists all installed tools with the specified output format
func ListTools(opts ListOptions) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	tools, err := installer.ListInstalledTools()
	if err != nil {
		return fmt.Errorf("failed to list installed tools: %w", err)
	}

	if len(tools) == 0 {
		if opts.JSON {
			core.MustFprintf(opts.Writer, "[]")
			return nil
		}

		core.MustFprintf(opts.Writer, "No tools installed.\n")
		core.MustFprintf(opts.Writer, "Install tools with: orla tool install TOOL-NAME\n")
		return nil
	}

	if opts.JSON {
		encoder := json.NewEncoder(opts.Writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(tools)
	}

	if opts.Verbose {
		w := tabwriter.NewWriter(opts.Writer, 0, 0, 2, ' ', 0)

		core.MustFprintf(w, "NAME\tVERSION\tDESCRIPTION\n")
		core.MustFprintf(w, "----\t-------\t-----------\n")

		for _, tool := range tools {
			description := tool.Description
			if len(description) > 60 {
				description = description[:57] + "..."
			}
			core.MustFprintf(w, "%s\t%s\t%s\n", tool.Name, tool.Version, description)
		}

		return w.Flush()
	}

	// Simple format by default: tool-name (version)
	for _, tool := range tools {
		core.MustFprintf(opts.Writer, "%s (%s)\n", tool.Name, tool.Version)
	}

	return nil
}
