package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/installer"
)

// newUninstallCmd creates the uninstall command
func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall TOOL-NAME",
		Short: "Remove an installed tool",
		Long: `Uninstall a tool by removing it from ~/.orla/tools/TOOL-NAME/.
This removes all versions of the tool.

Examples:
  orla uninstall fs
  orla uninstall http`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := args[0]

			if err := installer.UninstallTool(toolName); err != nil {
				return fmt.Errorf("failed to uninstall tool '%s': %w", toolName, err)
			}

			fmt.Printf("Successfully uninstalled tool '%s'\n", toolName)
			fmt.Println("Restart orla server for changes to take effect.")
			return nil
		},
	}

	return cmd
}
