package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/tool"
)

// newToolListCmd creates the tool list command
func newToolListCmd() *cobra.Command {
	var jsonOutput bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all installed tools with their versions and descriptions",
		Long: `List all installed tools and their versions. Tools are installed to
~/.orla/tools/TOOL-NAME/VERSION/ and are automatically discovered by the orla runtime.

By default, shows a simple list format. Use --verbose or --table to see detailed
information including descriptions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tool.ListTools(tool.ListOptions{
				JSON:    jsonOutput,
				Verbose: verbose,
				Writer:  os.Stdout,
			})
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information including descriptions")
	cmd.Flags().BoolVar(&verbose, "table", false, "Show detailed information in table format (alias for --verbose)")

	return cmd
}
