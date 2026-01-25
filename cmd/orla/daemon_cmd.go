package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/dorcha-inc/orla/internal/serving"
	servingapi "github.com/dorcha-inc/orla/internal/serving/api"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// newDaemonCmd creates the daemon command for the Agentic Serving Layer
func newDaemonCmd() *cobra.Command {
	var configPath string
	var listenAddress string
	var prettyLog bool

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the Agentic Serving Layer daemon (RFC 5)",
		Long: `Start the Agentic Serving Layer daemon that manages inference backends,
KV cache policies, and multi-agent coordination.

The daemon runs as a long-lived process and provides an HTTP API for agents to connect.
Agents can use the daemon to execute workflows, share context, and manage cache policies.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Resolve logging format: CLI flag wins; otherwise config
			resolvedPrettyLog := resolveLogFormat(cfg, prettyLog)

			// Initialize logger
			if err := core.Init(resolvedPrettyLog); err != nil {
				return fmt.Errorf("failed to initialize logger: %w", err)
			}

			// Check if agentic serving configuration exists
			if cfg.AgenticServing == nil {
				return fmt.Errorf("agentic_serving configuration is required")
			}

			listenAddress := "localhost:8081" // Default

			if cfg.AgenticServing.Daemon != nil && cfg.AgenticServing.Daemon.ListenAddress != "" {
				listenAddress = cfg.AgenticServing.Daemon.ListenAddress
			}

			// Create serving layer
			servingLayer, err := serving.NewLayer(cfg.AgenticServing)
			if err != nil {
				return fmt.Errorf("failed to create serving layer: %w", err)
			}

			// Create API server
			apiServer := servingapi.NewServer(servingLayer, listenAddress)

			// Set up signal handling for graceful shutdown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			// Start server in a goroutine
			errChan := make(chan error, 1)
			go func() {
				if err := apiServer.Start(); err != nil {
					errChan <- err
				}
			}()

			// Wait for signal or error
			select {
			case sig := <-sigChan:
				zap.L().Info("Received signal, shutting down",
					zap.String("signal", sig.String()))
				if err := apiServer.Shutdown(ctx); err != nil {
					return fmt.Errorf("failed to shutdown server: %w", err)
				}
			case err := <-errChan:
				if err != nil {
					return fmt.Errorf("server error: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file (default: uses precedence)")
	cmd.Flags().StringVarP(&listenAddress, "listen-address", "l", "", "Address to listen on (default: from config or localhost:8081)")
	cmd.Flags().BoolVar(&prettyLog, "pretty", false, "Use pretty-printed logs instead of JSON")

	return cmd
}
