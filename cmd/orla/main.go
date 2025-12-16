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
	var (
		configPath   string
		useStdio     bool
		prettyLog    bool
		portFlag     int
		toolsDirFlag string
	)

	rootCmd := &cobra.Command{
		Use:   "orla",
		Short: "Orla MCP server runtime",
		Long: `Orla is a runtime for Model Context Protocol (MCP) servers that automatically
discovers and executes tools from the filesystem.`,
		Version: fmt.Sprintf("%s (built: %s)", version, buildDate),
		// Default to serve command when no subcommand is provided (backward compatibility)
		RunE: func(cmd *cobra.Command, args []string) error {
			// If no subcommand, run serve with the flags that were passed
			return runServe(configPath, useStdio, prettyLog, portFlag, toolsDirFlag)
		},
	}

	// Add serve flags to root command for backward compatibility
	// These flags are also available on the "serve" subcommand
	rootCmd.Flags().StringVar(&configPath, "config", "", "Path to orla.yaml config file")
	rootCmd.Flags().IntVar(&portFlag, "port", 0, "Port to listen on (ignored if stdio is used)")
	rootCmd.Flags().BoolVar(&useStdio, "stdio", false, "Use stdio instead of TCP port")
	rootCmd.Flags().BoolVar(&prettyLog, "pretty", false, "Use pretty-printed logs instead of JSON")
	rootCmd.Flags().StringVar(&toolsDirFlag, "tools-dir", "", "Directory containing tools (overrides config file)")

	// Add subcommands
	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newInstallCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
