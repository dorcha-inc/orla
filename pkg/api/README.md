# Orla Daemon API Client

This package provides a public Go client library for interacting with the Orla Agentic Serving Layer daemon API (RFC 5).

## Overview

The Orla daemon is a control plane that coordinates multi-agent workflows, manages shared context, and enforces KV cache policies. This client library enables external code to:

- Execute workflows with multi-agent coordination
- Manage shared context across agents
- Build custom multi-agent experiments

## Installation

```go
import "github.com/dorcha-inc/orla/pkg/api"
```

## Quick Start

### Simple Workflow Execution

The easiest way to execute a workflow is using the high-level `WorkflowExecutor`:

```go
package main

import (
    "context"
    "fmt"
    "github.com/dorcha-inc/orla/pkg/api"
)

func main() {
    // Create workflow executor
    executor := api.NewWorkflowExecutor("http://localhost:8081")
    
    // Execute workflow (daemon handles inference)
    responses, err := executor.ExecuteWorkflow(ctx, "story_finishing_game", "Once upon a time", 100)
    if err != nil {
        panic(err)
    }
    
    // Process responses
    for i, resp := range responses {
        fmt.Printf("Task %d: %s\n", i+1, resp.Content)
    }
}
```

### Workflow Execution

The daemon automatically reads `orla.yaml` and resolves server names from agent profiles, so no config is needed:

```go
executor := api.NewWorkflowExecutor("http://localhost:8081")
responses, err := executor.ExecuteWorkflow(ctx, "story_finishing_game", "Once upon a time", 100)
```

**Note**: The daemon reads `orla.yaml` and automatically resolves LLM server names from agent profiles. Shared context is configured at the LLM server level in `orla.yaml` (`context.shared: true`), and the daemon handles all shared context logic.

### Low-Level API

For more control, use the client directly:

```go
client := api.NewClient("http://localhost:8081")

// Check daemon health
if err := client.Health(ctx); err != nil {
    panic(fmt.Sprintf("Daemon not healthy: %v", err))
}

// Start a workflow
execID, err := client.StartWorkflow(ctx, "story_finishing_game")
if err != nil {
    panic(err)
}

// Execute tasks until complete
for {
    task, taskIndex, complete, err := client.GetNextTask(ctx, execID)
    if err != nil {
        panic(err)
    }
    
    if complete {
        break
    }
    
    // Execute the task
    prompt := fmt.Sprintf("Continue the story: %s", context)
    response, err := client.ExecuteTask(ctx, execID, taskIndex, prompt, 100)
    if err != nil {
        panic(err)
    }
    
    // Mark task as complete
    err = client.CompleteTask(ctx, execID, taskIndex, response)
    if err != nil {
        panic(err)
    }
    
    context += " " + response.Content
}
```

## API Reference

### Client

```go
client := api.NewClient(baseURL string) *Client
```

Creates a new daemon API client for low-level operations.

### WorkflowExecutor

```go
executor := api.NewWorkflowExecutor(daemonURL string) *WorkflowExecutor
```

Creates a high-level workflow executor. The daemon automatically reads `orla.yaml` and resolves server names from agent profiles, so no config is needed.


### Health Check

```go
err := client.Health(ctx context.Context) error
```

Checks if the daemon is healthy and reachable.

### Workflow Management

#### StartWorkflow

```go
executionID, err := client.StartWorkflow(ctx context.Context, workflowName string) (string, error)
```

Starts a workflow execution and returns the execution ID.

#### GetNextTask

```go
task, taskIndex, complete, err := client.GetNextTask(ctx context.Context, executionID string) (*WorkflowTask, int, bool, error)
```

Retrieves the next task to execute from a workflow. Returns:
- `task`: The workflow task to execute
- `taskIndex`: The index of the task
- `complete`: Whether the workflow is complete
- `error`: Any error that occurred

#### ExecuteTask

```go
response, err := client.ExecuteTask(ctx context.Context, executionID string, taskIndex int, prompt string, maxTokens int) (*TaskResponse, error)
```

Executes a workflow task. The daemon handles inference, context management, and cache policies. `maxTokens` is optional (pass 0 to omit).

#### CompleteTask

```go
err := client.CompleteTask(ctx context.Context, executionID string, taskIndex int, response *TaskResponse) error
```

Marks a task as complete and reports the response to the daemon.

### Context Management

#### GetContext

```go
messages, err := client.GetContext(ctx context.Context, serverName string) ([]Message, error)
```

Retrieves shared context for a given LLM server.

#### SyncContext

```go
err := client.SyncContext(ctx context.Context, serverName string, messages []Message) error
```

Syncs local context with the daemon for a given LLM server.

### Agent Execution

For agent execution with tool support:

```go
import "github.com/modelcontextprotocol/go-sdk/mcp"

executor := api.NewAgentExecutor("http://localhost:8081")

// Single inference call (no tool loop)
req := &api.AgentExecuteRequest{
    ProfileName: "my_agent",
    Prompt:      "What's the weather?",
    Tools:       []*mcp.Tool{...}, // From MCP client
}
response, err := executor.Execute(ctx, req)
```

### Agent Execution with Tools (Full Agent Loop)

For full agent loop with tool calling, pass a `*mcp.ClientSession` directly:

```go
import (
    "github.com/modelcontextprotocol/go-sdk/mcp"
    "github.com/dorcha-inc/orla/pkg/api"
)

executor := api.NewAgentExecutor("http://localhost:8081")

// Create your MCP client session
var mcpSession *mcp.ClientSession // Your MCP client session

response, err := executor.ExecuteWithTools(
    ctx,
    "my_agent",
    "What's the weather in Boston?",
    mcpSession,
    10, // max iterations
    func(iteration int, resp *api.TaskResponse) error {
        fmt.Printf("Iteration %d: %s\n", iteration, resp.Content)
        return nil
    },
)
```

**Note**: Tools are handled client-side via MCP using the official MCP SDK's `ClientSession`. The daemon handles inference; the client handles the agent loop (tool calling, iteration).

## Example: Story Finishing Game

See `examples/story_finishing.go` for a complete example of using this API to run multi-agent experiments.
