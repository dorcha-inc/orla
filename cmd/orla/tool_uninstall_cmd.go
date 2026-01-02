package main

import (
	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/tool"
)

// newToolUninstallCmd creates the tool uninstall command
func newToolUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall TOOL-NAME",
		Short: "Remove an installed tool",
		Long: `Uninstall a tool by removing it from ~/.orla/tools/TOOL-NAME/.
This removes all versions of the tool.

Examples:
  orla tool uninstall fs
  orla tool uninstall http`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return tool.UninstallTool(args[0])
		},
	}

	return cmd
}
