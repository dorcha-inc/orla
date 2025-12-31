package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/dorcha-inc/orla/internal/tool"
)

// newToolSearchCmd creates the tool search command
func newToolSearchCmd() *cobra.Command {
	var registryURL string
	var verbose bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search the registry for tools matching the query",
		Long: `Search the registry for tools matching the query. The search looks in tool
names, descriptions, and keywords (case-insensitive).

By default, shows a simple list format. Use --verbose or --table to see detailed
information in a table format.

Examples:
  orla tool search filesystem
  orla tool search http
  orla tool search --registry https://github.com/user/custom-registry query`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return tool.SearchTools(args[0], tool.SearchOptions{
				RegistryURL: registryURL,
				Verbose:     verbose,
				JSON:        jsonOutput,
				Writer:      os.Stdout,
			})
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", fmt.Sprintf("Registry URL (default: %s)", registry.DefaultRegistryURL))
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information in table format")
	cmd.Flags().BoolVar(&verbose, "table", false, "Show detailed information in table format (alias for --verbose)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}
