package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
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
	// set up zap logger - explicitly write to stderr to avoid interfering with tool stdout
	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"stderr"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck // Ignore close errors on logger
	zap.ReplaceGlobals(logger)

	rootCmd := &cobra.Command{
		Use:   "orla-test",
		Short: "Test orla MCP tools via HTTP or stdio transports",
		Long: `orla-test is a CLI tool for testing orla MCP tools.

It supports both HTTP and stdio transports and can initialize sessions,
call tools, and display their output.`,
		Version: fmt.Sprintf("%s (built: %s)", version, buildDate),
	}

	// add commands
	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newCallCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
