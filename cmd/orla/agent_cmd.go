package main

import (
	"github.com/dorcha-inc/orla/internal/agent"
	"github.com/spf13/cobra"
)

// newAgentCmd creates the agent command for one-shot execution
func newAgentCmd() *cobra.Command {
	var modelFlag string
	var daemonURL string
	var profileName string
	var workflowName string

	cmd := &cobra.Command{
		Use:   "agent <prompt>",
		Short: "Execute a one-shot agent prompt",
		Long: `Execute a one-shot agent prompt. Orla processes the prompt, selects
and invokes appropriate tools, and returns the result.

This command supports streaming output by default.

The prompt is provided as a single argument. If the prompt contains spaces,
quote it in the shell. For example:
  orla agent "list files in the current directory"
  orla agent "generate a Dockerfile for this repo"
  orla agent hello  # Single word, no quotes needed

You can also pipe input to the command:
  cat file.txt | orla agent "summarize this"
  orla agent "summarize this" < file.txt

When using the Agentic Serving Layer (RFC 5), you can specify:
  --daemon <url>     Connect to an existing daemon
  --profile <name>   Use an agent profile from config
  --workflow <name>  Execute a workflow instead of a single prompt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Execute agent prompt (all logic is in agent package, including stdin reading)
			return agent.ExecuteAgentPrompt(args[0], modelFlag, daemonURL, profileName, workflowName)
		},
	}

	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "Model to use (e.g., ollama:llama3)")
	cmd.Flags().StringVarP(&daemonURL, "daemon", "d", "", "Daemon URL to connect to (e.g., http://localhost:8081)")
	cmd.Flags().StringVarP(&profileName, "profile", "p", "", "Agent profile name from config")
	cmd.Flags().StringVarP(&workflowName, "workflow", "w", "", "Workflow name to execute")

	return cmd
}
