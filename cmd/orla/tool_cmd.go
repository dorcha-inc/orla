package main

import (
	"github.com/spf13/cobra"
)

// newToolCmd creates the tool command group
func newToolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool",
		Short: "Manage Orla tools",
		Long: `Manage tools for Orla. Tools are executable packages that extend Orla's
capabilities. Install tools from the registry, list installed tools, search
for available tools, and manage tool versions.`,
	}

	// Add subcommands
	cmd.AddCommand(newToolListCmd())
	cmd.AddCommand(newToolInstallCmd())
	cmd.AddCommand(newToolUninstallCmd())
	cmd.AddCommand(newToolSearchCmd())
	cmd.AddCommand(newToolInfoCmd())
	cmd.AddCommand(newToolUpdateCmd())

	return cmd
}
