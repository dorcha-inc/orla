package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/server"
	"github.com/dorcha-inc/orla/internal/state"
)

type Flags struct {
	configPath   string
	useStdio     bool
	prettyLog    bool
	portFlag     int
	toolsDirFlag string
}

// parseFlags parses command-line flags and returns their values
func parseFlags() Flags {
	flags := Flags{}
	flag.StringVar(&flags.configPath, "config", "", "Path to orla.yaml config file")
	flag.IntVar(&flags.portFlag, "port", 0, "Port to listen on (ignored if stdio is used)")
	flag.BoolVar(&flags.useStdio, "stdio", false, "Use stdio instead of TCP port")
	flag.BoolVar(&flags.prettyLog, "pretty", false, "Use pretty-printed logs instead of JSON")
	flag.StringVar(&flags.toolsDirFlag, "tools-dir", "", "Directory containing tools (overrides config file)")
	flag.Parse()
	return flags
}

// loadConfig loads configuration from a file path, or returns defaults if path is empty
// Per RFC 1 section 6.3: if no config path is specified, check for orla.yaml in current directory
func loadConfig(configPath string) (*state.OrlaConfig, error) {
	if configPath == "" {
		// Check for orla.yaml in current directory (RFC 1 section 6.3)
		if _, err := os.Stat("orla.yaml"); err == nil {
			zap.L().Info("Found orla.yaml in current directory, using it")
			return state.NewOrlaConfigFromPath("orla.yaml")
		}
		// No config file found, use defaults
		return state.NewDefaultOrlaConfig()
	}
	return state.NewOrlaConfigFromPath(configPath)
}

// resolveLogFormat determines the log format based on CLI flag and config
func resolveLogFormat(cfg *state.OrlaConfig, prettyLog bool) bool {
	if !prettyLog && cfg.LogFormat == "pretty" {
		return true
	}
	return prettyLog
}

// validateAndApplyPort validates the port flag and applies port logic to config
func validateAndApplyPort(cfg *state.OrlaConfig, portFlag int, useStdio bool) error {
	if portFlag < 0 {
		return fmt.Errorf("port must be a positive integer (or 0 to remain unset), got %d", portFlag)
	}

	// Command line flag overrides config file
	if portFlag != 0 {
		cfg.Port = portFlag
	}

	if !useStdio && portFlag == 0 && cfg.Port == 0 {
		cfg.Port = 8080
	}

	return nil
}

// setupSignalHandling sets up signal handling for hot reload and graceful shutdown
func setupSignalHandling(ctx context.Context, srv *server.OrlaServer) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			sig := <-sigChan
			switch sig {
			case syscall.SIGHUP:
				zap.L().Info("Received SIGHUP, reloading configuration and tools")
				if err := srv.Reload(); err != nil {
					zap.L().Error("Failed to reload", zap.Error(err))
				} else {
					zap.L().Info("Successfully reloaded configuration and tools")
				}
			case syscall.SIGINT, syscall.SIGTERM:
				zap.L().Info("Received shutdown signal")
				cancel()
				return
			}
		}
	}()

	return ctx, cancel
}

// runServer starts the server in either stdio or HTTP mode
func runServer(ctx context.Context, srv *server.OrlaServer, useStdio bool, cfg *state.OrlaConfig) error {
	if useStdio {
		zap.L().Info("Starting orla server on stdio")
		return srv.ServeStdio(ctx)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	zap.L().Info("Starting orla server", zap.String("address", addr))
	return srv.Serve(ctx, addr)
}

// testableMain is a testable version of main that can be used to test the server startup logic
// It returns an error if the server startup logic fails and we should exit the program.
func testableMain(ctx context.Context) error {
	flags := parseFlags()

	// Load configuration (defaults if none provided)
	cfg, err := loadConfig(flags.configPath)
	if err != nil {
		fmt.Printf("Failed to load configuration: %v", err)
		return err
	}

	// Apply tools directory override if provided via flag (before creating server)
	if flags.toolsDirFlag != "" {
		if err := cfg.SetToolsDir(flags.toolsDirFlag); err != nil {
			return err
		}
	}

	// Resolve logging format: CLI flag wins; otherwise config
	_ = resolveLogFormat(cfg, flags.prettyLog)

	// Initialize global logger
	if err := core.Init(flags.prettyLog); err != nil {
		fmt.Printf("Failed to initialize logger: %v", err)
		return err
	}
	defer zap.L().Sync() //nolint:errcheck // Ignore sync errors on stdout/stderr, they're not critical and common in test environments

	// Validate and apply port configuration
	if err := validateAndApplyPort(cfg, flags.portFlag, flags.useStdio); err != nil {
		fmt.Printf("%s\n", err)
		return err
	}

	// Create server (after all config overrides are applied)
	srv := server.NewOrlaServer(cfg, flags.configPath)

	// Set up signal handling for hot reload
	ctx, cancel := setupSignalHandling(ctx, srv)
	defer cancel()

	// Start server
	if err := runServer(ctx, srv, flags.useStdio, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			zap.L().Info("Server context canceled, exiting gracefully")
			return nil
		}

		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func main() {
	if err := testableMain(context.Background()); err != nil {
		zap.L().Fatal("Fatal server error", zap.Error(err))
	}
}
