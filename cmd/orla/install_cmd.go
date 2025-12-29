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
		Use:   "install [TOOL-NAME]",
		Short: "Install a tool from the registry or local path",
		Long: `Install a tool from the registry or a local directory. The tool will be installed
to ~/.orla/tools/TOOL-NAME/VERSION/ and automatically registered with the orla runtime.

When using --local, TOOL-NAME should not be provided as it will be read from the tool.yaml manifest.

Examples:
  orla install fs
  orla install fs@0.1.0
  orla install fs --version latest
  orla install --local ./path/to/tool`,
		Args: func(cmd *cobra.Command, args []string) error {
			// Check if --local flag is set
			localFlag, getLocalFlagErr := cmd.Flags().GetString("local")
			if getLocalFlagErr != nil {
				return fmt.Errorf("failed to get local flag: %w", getLocalFlagErr)
			}

			// If --local is used, no tool name is required
			if localFlag != "" {
				if len(args) > 0 {
					return fmt.Errorf("tool name should not be provided when using --local (tool name is read from tool.yaml)")
				}
				return nil
			}

			// Otherwise, tool name is required
			if len(args) == 0 {
				return fmt.Errorf("tool name is required when installing from registry")
			}

			if len(args) > 1 {
				return fmt.Errorf("at most one argument (TOOL-NAME) allowed when installing from registry")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle local installation
			if localPath != "" {
				if err := installer.InstallLocalTool(localPath, os.Stdout); err != nil {
					return fmt.Errorf("failed to install local tool: %w", err)
				}
				fmt.Println("Tool is now available. Restart orla server to use it.")
				return nil
			}

			// Args validation (above) ensures tool name is provided when not using --local
			// so we can safely access the first argument here.
			toolName := args[0]

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
	cmd.Flags().StringVar(&localPath, "local", "", "Install from local directory or archive (tool name will be read from tool.yaml)")

	return cmd
}
