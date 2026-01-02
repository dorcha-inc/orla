package tool

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/registry"
)

// SearchOptions configures the output format for searching tools
type SearchOptions struct {
	RegistryURL string
	Verbose     bool
	JSON        bool
	Writer      io.Writer
}

// SearchTools searches the registry for tools matching the query
func SearchTools(query string, opts SearchOptions) error {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	// Use default registry if not specified
	if opts.RegistryURL == "" {
		opts.RegistryURL = registry.DefaultRegistryURL
	}

	// Fetch registry
	reg, err := registry.FetchRegistry(opts.RegistryURL, true)
	if err != nil {
		return fmt.Errorf("failed to fetch registry: %w", err)
	}

	// Search for tools
	results := registry.SearchTools(reg, query)

	if len(results) == 0 {
		if opts.JSON {
			core.MustFprintf(opts.Writer, "[]")
			return nil
		}

		core.MustFprintf(opts.Writer, "No tools found matching '%s'\n", query)
		return nil
	}

	if opts.JSON {
		encoder := json.NewEncoder(opts.Writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	if opts.Verbose {
		w := tabwriter.NewWriter(opts.Writer, 0, 0, 2, ' ', 0)

		if _, err := fmt.Fprintln(w, "NAME\tDESCRIPTION"); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		if _, err := fmt.Fprintln(w, "----\t-----------"); err != nil {
			return fmt.Errorf("failed to write separator: %w", err)
		}

		for _, tool := range results {
			description := tool.Description
			if len(description) > 60 {
				description = description[:57] + "..."
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\n", tool.Name, description); err != nil {
				return fmt.Errorf("failed to write row: %w", err)
			}
		}

		if err := w.Flush(); err != nil {
			return fmt.Errorf("failed to flush writer: %w", err)
		}
	} else {
		// Simple format by default: tool-name: description
		for _, tool := range results {
			description := tool.Description
			if len(description) > 80 {
				description = description[:77] + "..."
			}
			core.MustFprintf(opts.Writer, "%s: %s\n", tool.Name, description)
		}
	}

	if !opts.JSON {
		core.MustFprintf(opts.Writer, "\nInstall a tool with: orla tool install TOOL-NAME\n")
	}

	return nil
}
