package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/tool"
)

// newToolInfoCmd creates the tool info command
func newToolInfoCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "info TOOL-NAME",
		Short: "Display detailed information about a tool",
		Long: `Display detailed information about an installed tool, including description,
version, dependencies, permissions, and other metadata from the tool.yaml manifest.

Examples:
  orla tool info fs
  orla tool info http`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return tool.GetToolInfo(args[0], tool.InfoOptions{
				JSON:   jsonOutput,
				Writer: os.Stdout,
			})
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}
