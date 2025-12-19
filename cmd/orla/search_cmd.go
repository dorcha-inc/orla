package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/registry"
)

// newSearchCmd creates the search command
func newSearchCmd() *cobra.Command {
	var registryURL string
	var verbose bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Search the registry for tools",
		Long: `Search the registry for tools matching the query. The search looks in tool
names, descriptions, and keywords (case-insensitive).

By default, shows a simple list format. Use --verbose or --table to see detailed
information in a table format.

Examples:
  orla search filesystem
  orla search http
  orla search --registry https://github.com/user/custom-registry query`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Use default registry if not specified
			if registryURL == "" {
				registryURL = registry.DefaultRegistryURL
			}

			// Fetch registry
			reg, err := registry.FetchRegistry(registryURL, true)
			if err != nil {
				return fmt.Errorf("failed to fetch registry: %w", err)
			}

			// Search for tools
			results := registry.SearchTools(reg, query)

			if len(results) == 0 {
				if jsonOutput {
					fmt.Println("[]")
					return nil
				}

				fmt.Printf("No tools found matching '%s'\n", query)
				return nil
			}

			if jsonOutput {
				// JSON output for machine-readable format
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(results)
			}

			if verbose {
				// Verbose table format
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

				_, errWriteHeader := fmt.Fprintln(w, "NAME\tDESCRIPTION")
				if errWriteHeader != nil {
					return fmt.Errorf("failed to write header: %w", errWriteHeader)
				}

				_, errWriteSeparator := fmt.Fprintln(w, "----\t-----------")
				if errWriteSeparator != nil {
					return fmt.Errorf("failed to write separator: %w", errWriteSeparator)
				}

				for _, tool := range results {
					description := tool.Description
					if len(description) > 60 {
						description = description[:57] + "..."
					}
					_, errWriteRow := fmt.Fprintf(w, "%s\t%s\n", tool.Name, description)
					if errWriteRow != nil {
						return fmt.Errorf("failed to write row: %w", errWriteRow)
					}
				}

				if errFlush := w.Flush(); errFlush != nil {
					return fmt.Errorf("failed to flush writer: %w", errFlush)
				}
			} else {
				// Simple format by default: tool-name: description
				for _, tool := range results {
					description := tool.Description
					if len(description) > 80 {
						description = description[:77] + "..."
					}
					fmt.Printf("%s: %s\n", tool.Name, description)
				}
			}

			if !jsonOutput {
				fmt.Printf("\nInstall a tool with: orla install TOOL-NAME\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", fmt.Sprintf("Registry URL (default: %s)", registry.DefaultRegistryURL))
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information in table format")
	cmd.Flags().BoolVar(&verbose, "table", false, "Show detailed information in table format (alias for --verbose)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}
