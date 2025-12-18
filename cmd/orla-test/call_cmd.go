package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// newCallCmd creates a new command to call an MCP tool
func newCallCmd() *cobra.Command {
	var (
		transport string
		port      int
		toolName  string
		argsJSON  string
		stdin     string
		orlaBin   string
	)

	cmd := &cobra.Command{
		Use:   "call [flags]",
		Short: "Call an MCP tool",
		Long: `Call an MCP tool and display its output.

The tool can be called via HTTP or stdio transport. For HTTP transport,
a session will be automatically initialized. For stdio transport, the
orla binary will be spawned and communication happens via stdin/stdout.`,
		Example: `  # Call a tool via HTTP
  orla-test call --transport http --port 8080 --tool hello --args '{"name":"World"}'

  # Call a tool with stdin input
  orla-test call --transport http --tool greet --args '{"language":"es"}' --stdin "María"

  # Call a tool via stdio
  orla-test call --transport stdio --tool hello --args '{"name":"World"}'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if toolName == "" {
				return fmt.Errorf("tool name is required (use --tool)")
			}

			var toolArgs map[string]any
			if argsJSON == "" {
				toolArgs = make(map[string]any)
			} else {
				if err := json.Unmarshal([]byte(argsJSON), &toolArgs); err != nil {
					return fmt.Errorf("invalid JSON in --args: %w", err)
				}
			}

			// Add stdin to arguments if provided
			if stdin != "" {
				toolArgs["stdin"] = stdin
			}

			var output string
			var err error

			switch transport {
			case "http":
				output, err = testToolHTTP(port, toolName, toolArgs)
			case "stdio":
				// Ensure orla binary is available for stdio transport
				if orlaBin == "" {
					binPath, binErr := ensureOrlaBinary()
					if binErr != nil {
						return fmt.Errorf("orla binary not found in PATH (required for stdio transport): %w", binErr)
					}
					orlaBin = binPath
				}
				output, err = testToolStdio(orlaBin, toolName, toolArgs)
			default:
				return fmt.Errorf("unknown transport: %s (supported: http, stdio)", transport)
			}

			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVarP(&transport, "transport", "t", "http", "Transport to use (http or stdio)")
	cmd.Flags().IntVarP(&port, "port", "p", 8080, "HTTP port (ignored for stdio)")
	cmd.Flags().StringVarP(&toolName, "tool", "n", "", "Tool name to call (required)")
	cmd.Flags().StringVarP(&argsJSON, "args", "a", "{}", "Tool arguments as JSON")
	cmd.Flags().StringVarP(&stdin, "stdin", "s", "", "Stdin input for tool")
	cmd.Flags().StringVar(&orlaBin, "orla-bin", "", "Path to orla binary (for stdio transport, default: auto-detect)")

	if err := cmd.MarkFlagRequired("tool"); err != nil {
		// This should never happen, but handle it gracefully
		fmt.Fprintf(os.Stderr, "Warning: failed to mark tool flag as required: %v\n", err)
	}

	return cmd
}

func testToolHTTP(port int, toolName string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "orla-test",
		Version: "1.0.0",
	}, nil)

	// Create HTTP transport
	transport := &mcp.StreamableClientTransport{
		Endpoint: fmt.Sprintf("http://localhost:%d/mcp", port),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Connect to server (this initializes the session)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}
	defer session.Close() //nolint:errcheck // Ignore close errors on session

	// Call tool
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("tool call failed: %w", err)
	}

	return extractToolOutput(result), nil
}

func testToolStdio(orlaBin string, toolName string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "orla-test",
		Version: "1.0.0",
	}, nil)

	// Create stdio transport (spawns orla process)
	transport := &mcp.CommandTransport{
		Command: exec.Command(orlaBin, "serve", "--stdio"),
	}

	// Connect to server
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect: %w", err)
	}
	defer session.Close() //nolint:errcheck // Ignore close errors on session

	// Call tool
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("tool call failed: %w", err)
	}

	return extractToolOutput(result), nil
}

func extractToolOutput(result *mcp.CallToolResult) string {
	// Try structuredContent.stdout first
	if result.StructuredContent != nil {
		if structuredMap, ok := result.StructuredContent.(map[string]any); ok {
			if stdout, ok := structuredMap["stdout"].(string); ok && stdout != "" {
				return stdout
			}
		}
	}

	// Fall back to content[0].text
	if len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(*mcp.TextContent); ok && textContent.Text != "" {
			return textContent.Text
		}
	}

	return ""
}
