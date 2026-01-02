package tool

import (
	"fmt"
	"os"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/installer"
)

// UninstallTool removes an installed tool
func UninstallTool(toolName string) error {
	// Load config to get ToolsDir (handles project > user > default precedence)
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.ToolsDir == "" {
		return fmt.Errorf("tools directory not configured")
	}
	toolsDir := cfg.ToolsDir

	if err := installer.UninstallTool(toolName, toolsDir); err != nil {
		return fmt.Errorf("failed to uninstall tool '%s': %w", toolName, err)
	}

	core.MustFprintf(os.Stdout, "Successfully uninstalled tool '%s'\n", toolName)
	core.MustFprintf(os.Stdout, "Restart orla server for changes to take effect.\n")
	return nil
}
