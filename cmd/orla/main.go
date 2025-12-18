package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	// build time date
	buildDate = "unknown"
)

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

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
