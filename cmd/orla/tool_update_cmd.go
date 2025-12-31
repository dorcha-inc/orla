package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/dorcha-inc/orla/internal/tool"
)

// newToolUpdateCmd creates the tool update command
func newToolUpdateCmd() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "update TOOL-NAME",
		Short: "Update a tool to the latest version",
		Long: `Update an installed tool to the latest version from the registry.
This will download and install the latest version while keeping the old version
until the update is complete.

Examples:
  orla tool update fs
  orla tool update http --registry https://github.com/user/custom-registry`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return tool.UpdateTool(args[0], tool.UpdateOptions{
				RegistryURL: registryURL,
				Writer:      os.Stdout,
			})
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", fmt.Sprintf("Registry URL (default: %s)", registry.DefaultRegistryURL))

	return cmd
}
