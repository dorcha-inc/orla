package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/dorcha-inc/orla/internal/registry"
)

// newUpdateCmd creates the update command
func newUpdateCmd() *cobra.Command {
	var registryURL string

	cmd := &cobra.Command{
		Use:   "update TOOL-NAME",
		Short: "Update a tool to the latest version",
		Long: `Update an installed tool to the latest version from the registry.
This will download and install the latest version while keeping the old version
until the update is complete.

Examples:
  orla update fs
  orla update http --registry https://github.com/user/custom-registry`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := args[0]

			// Use default registry if not specified
			if registryURL == "" {
				registryURL = registry.DefaultRegistryURL
			}

			// Update the tool
			if err := installer.UpdateTool(registryURL, toolName, os.Stdout); err != nil {
				return fmt.Errorf("failed to update tool: %w", err)
			}

			fmt.Printf("✓ Successfully updated %s to latest version\n", toolName)
			fmt.Println("Restart orla server to use the updated version.")
			return nil
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", fmt.Sprintf("Registry URL (default: %s)", registry.DefaultRegistryURL))

	return cmd
}
