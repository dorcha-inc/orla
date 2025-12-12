package main

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// newInitCmd creates a new command to initialize an MCP session (HTTP transport only)
func newInitCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize an MCP session (HTTP transport only)",
		Long: `Initialize a new MCP session and print the session ID.

This command only works with HTTP transport. For stdio transport,
sessions are handled automatically.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessionID, err := initSessionHTTP(port)
			if err != nil {
				return fmt.Errorf("failed to initialize session: %w", err)
			}
			fmt.Print(sessionID)
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "HTTP port")

	return cmd
}

// initSessionHTTP initializes an MCP session via HTTP using the MCP SDK
func initSessionHTTP(port int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "orla-test",
		Version: "1.0.0",
	}, nil)

	// Create HTTP transport
	transport := &mcp.StreamableClientTransport{
		Endpoint: fmt.Sprintf("http://localhost:%d/mcp", port),
	}

	// Connect to server (this initializes the session)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect to orla server on port %d: %w (is orla running? try 'make run' in the example directory)", port, err)
	}

	// Get session ID
	sessionID := session.ID()
	if sessionID == "" {
		errClose := session.Close()
		if errClose != nil {
			return "", fmt.Errorf("failed to close session: %w", err)
		}
		return "", fmt.Errorf("no session ID returned (is orla running on port %d? try 'make run' in the example directory)", port)
	}

	// Close session - we just wanted the ID
	err = session.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close session: %w", err)
	}

	return sessionID, nil
}
