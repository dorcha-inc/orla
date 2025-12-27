package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/installer"
	"github.com/dorcha-inc/orla/internal/registry"
)

// newInstallCmd creates the install command
func newInstallCmd() *cobra.Command {
	var (
		registryURL string
		version     string
		localPath   string
	)

	cmd := &cobra.Command{
		Use:   "install TOOL-NAME",
		Short: "Install a tool from the registry",
		Long: `Install a tool from the registry. The tool will be downloaded and installed
to ~/.orla/tools/TOOL-NAME/VERSION/ and automatically registered with the orla runtime.

Examples:
  orla install fs
  orla install fs@0.1.0
  orla install fs --version latest
  orla install --local ./path/to/tool`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := args[0]

			// Handle local installation
			if localPath != "" {
				return fmt.Errorf("local installation not yet implemented")
			}

			// Use default registry if not specified
			if registryURL == "" {
				registryURL = registry.DefaultRegistryURL
			}

			// Use "latest" if version not specified
			if version == "" {
				version = "latest"
			}

			// Install the tool
			if err := installer.InstallTool(registryURL, toolName, version, os.Stdout); err != nil {
				return fmt.Errorf("failed to install tool: %w", err)
			}

			fmt.Printf("Successfully installed %s\n", toolName)
			fmt.Println("Tool is now available. Restart orla server to use it.")

			return nil
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", fmt.Sprintf("Registry URL (default: %s)", registry.DefaultRegistryURL))
	cmd.Flags().StringVar(&version, "version", "latest", "Version constraint (e.g., '0.1.0', 'latest', '^0.1.0')")
	cmd.Flags().StringVar(&localPath, "local", "", "Install from local path (not yet implemented)")

	return cmd
}
