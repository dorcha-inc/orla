package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dorcha-inc/orla/internal/registry"
	"github.com/dorcha-inc/orla/internal/tool"
)

// newToolInstallCmd creates the tool install command
func newToolInstallCmd() *cobra.Command {
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
  orla tool install fs
  orla tool install fs@0.1.0
  orla tool install fs --version latest
  orla tool install --local ./path/to/tool`,
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
			var toolName string
			if len(args) > 0 {
				toolName = args[0]
				// Handle version in tool name (e.g., "fs@0.1.0")
				if strings.Contains(toolName, "@") {
					parts := strings.SplitN(toolName, "@", 2)
					toolName = parts[0]
					if version == "" || version == "latest" {
						version = parts[1]
					}
				}
			}

			return tool.InstallTool(toolName, tool.InstallOptions{
				RegistryURL: registryURL,
				Version:     version,
				LocalPath:   localPath,
				Writer:      os.Stdout,
			})
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", fmt.Sprintf("Registry URL (default: %s)", registry.DefaultRegistryURL))
	cmd.Flags().StringVar(&version, "version", "latest", "Version constraint (e.g., '0.1.0', 'latest', '^0.1.0')")
	cmd.Flags().StringVar(&localPath, "local", "", "Install from local directory or archive (tool name will be read from tool.yaml)")

	return cmd
}
