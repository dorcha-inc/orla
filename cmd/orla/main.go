package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version   string // Set via -ldflags at build time
	buildDate string // Set via -ldflags at build time
)

func init() {
	if version == "" {
		version = "dev"
	}
	if buildDate == "" {
		buildDate = "unknown"
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "orla",
		Short: "Orla MCP server runtime",
		Long: `Orla is a runtime for Model Context Protocol (MCP) servers that automatically
discovers and executes tools from the filesystem.`,
		Version: fmt.Sprintf("%s (built: %s)", version, buildDate),
	}

	// Add subcommands
	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newInstallCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newSearchCmd())
	rootCmd.AddCommand(newUninstallCmd())
	rootCmd.AddCommand(newUpdateCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
