package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/installer"
)

// newListCmd creates the list command
func newListCmd() *cobra.Command {
	var jsonOutput bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed tools and versions",
		Long: `List all installed tools and their versions. Tools are installed to
~/.orla/tools/TOOL-NAME/VERSION/ and are automatically discovered by the orla runtime.

By default, shows a simple list format. Use --verbose or --table to see detailed
information including descriptions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			tools, err := installer.ListInstalledTools()
			if err != nil {
				return fmt.Errorf("failed to list installed tools: %w", err)
			}

			if len(tools) == 0 {
				if jsonOutput {
					fmt.Println("[]")
				} else {
					fmt.Println("No tools installed.")
					fmt.Println("Install tools with: orla install TOOL-NAME")
				}
				return nil
			}

			if jsonOutput {
				// JSON output for machine-readable format
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(tools)
			}

			if verbose {
				// Verbose table format with descriptions
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

				_, errWriteHeader := fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION")
				if errWriteHeader != nil {
					return fmt.Errorf("failed to write header: %w", errWriteHeader)
				}

				_, errWriteSeparator := fmt.Fprintln(w, "----\t-------\t-----------")
				if errWriteSeparator != nil {
					return fmt.Errorf("failed to write separator: %w", errWriteSeparator)
				}

				for _, tool := range tools {
					description := tool.Description
					if len(description) > 60 {
						description = description[:57] + "..."
					}
					_, errWriteRow := fmt.Fprintf(w, "%s\t%s\t%s\n", tool.Name, tool.Version, description)
					if errWriteRow != nil {
						return fmt.Errorf("failed to write row: %w", errWriteRow)
					}
				}

				return w.Flush()
			}

			// Simple format by default: tool-name (version)
			for _, tool := range tools {
				fmt.Printf("%s (%s)\n", tool.Name, tool.Version)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information including descriptions")
	cmd.Flags().BoolVar(&verbose, "table", false, "Show detailed information in table format (alias for --verbose)")

	return cmd
}
