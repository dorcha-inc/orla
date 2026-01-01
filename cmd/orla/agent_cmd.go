package main

import (
	"github.com/dorcha-inc/orla/internal/agent"
	"github.com/spf13/cobra"
)

// newAgentCmd creates the agent command for one-shot execution
func newAgentCmd() *cobra.Command {
	var modelFlag string

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
  orla agent hello  # Single word, no quotes needed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Execute agent prompt (all logic is in agent package)
			return agent.ExecuteAgentPrompt(args[0], modelFlag)
		},
	}

	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "Model to use (e.g., ollama:llama3)")

	return cmd
}
