package main

import (
	"fmt"
	"log"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/spf13/cobra"
	"log/slog"
)

var (
	// version is set via -ldflags at build time
	version string
	// buildDate is set via -ldflags at build time
	buildDate string
)

func applyVersionDefaults() {
	if version == "" {
		slog.Warn("version is not set, using default dev version")
		version = "dev"
	}
	if buildDate == "" {
		slog.Warn("buildDate is not set, using default unknown build date")
		buildDate = "unknown"
	}
}

func main() {
	// Initialize a minimal logger (error level) so zap.L() is never a no-op.
	// Commands re-initialize with config; agent defaults to quiet (error level) for clean output.
	if err := core.InitLogger(false, "error"); err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}

	applyVersionDefaults()

	rootCmd := &cobra.Command{
		Use:     "orla",
		Short:   "Orla agent engine and CLI",
		Long:    `Orla is an execution engine for building high performance agentic systems. Use "orla serve" to run the agent engine as a service or "orla agent" for one-shot agent runs.`,
		Version: fmt.Sprintf("%s (built: %s)", version, buildDate),
	}

	rootCmd.AddCommand(newServeCmd())
	rootCmd.AddCommand(newAgentCmd())

	if err := rootCmd.Execute(); err != nil {
		core.Fatal("Error executing root command", "error", err)
	}
}
