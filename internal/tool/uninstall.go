package tool

import (
	"fmt"
	"os"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
)

// UninstallTool removes an installed tool
func UninstallTool(toolName string) error {
	if err := installer.UninstallTool(toolName); err != nil {
		return fmt.Errorf("failed to uninstall tool '%s': %w", toolName, err)
	}

	core.MustFprintf(os.Stdout, "Successfully uninstalled tool '%s'\n", toolName)
	core.MustFprintf(os.Stdout, "Restart orla server for changes to take effect.\n")
	return nil
}
