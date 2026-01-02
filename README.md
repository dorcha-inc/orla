<div align="center">
  <a href="https://github.com/dorcha-inc/orla">
    <img src="share/orla_canva.png" alt="Orla Logo" width="400">
  </a>
  <br>
  <h3 align="center">A dead-simple unix tool for local AI.</h3>
</div>

<p align="center">
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green.svg" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/dorcha-inc/orla"><img src="https://goreportcard.com/badge/github.com/dorcha-inc/orla" alt="Go Report Card"></a>
  <a href="https://www.bestpractices.dev/projects/6573"><img src="https://www.bestpractices.dev/projects/6573/badge" alt="OpenSSF Best Practices"></a>
  <a href="https://github.com/dorcha-inc/orla/actions/workflows/build.yml"><img src="https://github.com/dorcha-inc/orla/actions/workflows/build.yml/badge.svg" alt="Build"></a>
  <a href="https://codecov.io/gh/dorcha-inc/orla"><img src="https://codecov.io/gh/dorcha-inc/orla/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="https://discord.gg/bzKYCFewPT"><img src="https://img.shields.io/badge/Discord-5865F2?style=flat&logo=discord&logoColor=white" alt="Discord"></a>
</p>

For decades, the command line has been the most powerful and productive environment for developers. Tools like `grep`, `curl`, and `git` are second nature. They are fast, reliable, and composable. However, the ecosystem around AI and AI agents currently feels like using a bloated monolithic piece of proprietary software with over-priced and kafkaesque licensing fees.

Orla is built on a simple premise: AI should be a (free software) tool you own, not a service you rent. Like the language we wrote it in (Go), it treats simplicity as a first-order priority. Orla is a unix tool designed for the command line that lets you run agents. Orla uses models running on your own machine and automatically discovers the tools you already have, making it powerful and private right out of the box. No setup, no API keys or subscriptions, no data centers.

## Features

1. Orla runs locally. Your data, queries, and tools never leave your machine without your explicit instruction. It's private by default.
2. Orla brings the power of modern LLMs to your terminal with a dead-simple interface. If you know how to use `grep`, you know how to use Orla.
3. Orla is free and open-source software. No subscriptions, no vendor lock-in. You are not the product.

All the amazing folks who have taken their time to contribute something cool to orla are listed in [CONTRIBUTORS.md](CONTRIBUTORS.md).

## Quick links

- [Getting Started](#getting-started)
- [Usage](#usage)
- [Configuration](#configuration)
- [Command Line Options](#command-line-options)
- [Community + Contributions](#community--contributions)
- [Roadmap](#roadmap)
- [Integration Guides](#integration-guides)

## Getting Started

### Installation

Make sure you have Go (1.25+) installed, then install Orla:

```bash
go install github.com/dorcha-inc/orla/cmd/orla@latest
```

Or build it locally by cloning this repository and running:

```bash
make build
```

Or install locally:

```bash
make install
```

## Usage

Orla supports two modes of operation: `agent` for direct terminal interaction, and `serve` for integration with MCP clients.

### Use `orla agent` on a terminal directly

The simplest way to use Orla is through `agent`. Just ask Orla to do something, and it will use local models to reason and execute commands:

You can do a one-shot task like this:

```bash
orla agent "summarize this code" < main.go
```

You can run it in a pipeline, like this:

```bash
cat data.json | orla agent "extract all email addresses" | sort -u
```

This lets you pipe context directly into orla. Here's a second example:

```bash
git status | orla agent "Draft a short, imperative-mood commit message for these changes"
```

You can install one of Orla's tools (`fs`) and do file operations like this:

```bash
orla tool install fs
orla agent "find all TODO comments in *.c files in `pwd`" > todos.txt
```

You can also override the model:

```bash
orla agent "List all files in the current directory" --model ollama:ministral-3:3b
```

### Use `orla serve` to integrate with other MCP clients

For integration with external MCP clients (like Claude Desktop), run Orla as a server:

Start server on default port (8080):

```bash
orla serve
```

Use stdio transport

```bash
orla serve --stdio
```

If no configuration file is specified, Orla will automatically check for `orla.yaml` in the current directory. If not found, default configuration is used.

You can hot reload Orla to refresh tools and configuration without restarting:

```bash
kill -HUP $(pgrep orla)
```

### Installing Tools from the Registry

The easiest way to get started is to install tools from the [Orla Tool Registry](https://github.com/dorcha-inc/orla-registry):

Install the latest version of a tool
```bash
orla install fs
```

Install a specific version

```bash
orla install coinflip --version v0.1.0
```

Search for available tools

```bash
orla search $search_term
```

Installed tools are automatically placed in the default tools directory and will be discovered by Orla when you start the server or use agent mode.

### Creating Custom Tools

You can also create your own tools. Any executable can be a tool:

```bash
mkdir tools
cat > tools/hello.sh << 'EOF'
#!/bin/bash
echo "Hello from orla!"
EOF
chmod +x tools/hello.sh
```

Orla will automatically discover and make these tools available.

## Configuration

Orla works out of the box with zero configuration, but you can customize it with a YAML config file. Configuration follows a precedence order:

1. Environment variables (highest precedence) - e.g., `ORLA_PORT=3000`
2. Project config (`./orla.yaml` in current directory)
3. User config (`~/.orla/config.yaml`)
4. Orla's Defaults (lowest precedence)

If you create an `orla.yaml` file in your project directory, it will override the global user config for that project. This allows project-specific settings while maintaining global defaults.

### Configuration Options

#### MCP Server options

- `tools_dir`: Directory containing executable tools (default: `.orla/tools`)
- `port`: HTTP server port (default: `8080`, ignored in stdio mode)
- `timeout`: Tool execution timeout in seconds (default: `30`)
- `log_format`: `"json"` or `"pretty"` (default: `"json"`)
- `log_level`: `"debug"`, `"info"`, `"warn"`, `"error"`, or `"fatal"` (default: `"info"`)
- `log_file`: Optional log file path (default: empty, logs to stderr)

#### Orla Agent options

- `model`: Model identifier (e.g., `"ollama:ministral-3:3b"`, `"ollama:qwen3:1.7b"`) (default: `"ollama:qwen3:1.7b"`)
- `max_tool_calls`: Maximum tool calls per prompt (default: `10`)
- `streaming`: Enable streaming responses (default: `true`)
- `output_format`: Output format - `"auto"`, `"rich"`, or `"plain"` (default: `"auto"`)
- `confirm_destructive`: Prompt for confirmation on destructive actions (default: `true`)
- `dry_run`: Default to dry-run mode (default: `false`)
- `show_thinking`: Show thinking trace output for thinking-capable models (default: `false`)
- `show_tool_calls`: Show detailed tool call information (default: `false`)
- `show_progress`: Show progress messages even when UI is disabled (e.g., when stdin is piped) (default: `false`)

### Example Configuration

Create an `orla.yaml` file in your project directory:

```yaml
# Server mode configuration
tools_dir: ./tools
port: 8080
timeout: 30
log_format: json
log_level: info

# Agent mode configuration
model: ollama:llama3
max_tool_calls: 10
streaming: true
output_format: auto
confirm_destructive: true
show_thinking: false
show_tool_calls: true
```

You can also set configuration via environment variables. For example:

```bash
export ORLA_PORT=3000
export ORLA_MODEL=ollama:qwen3:1.7b
export ORLA_SHOW_TOOL_CALLS=true
```

## Git hooks

orla includes pre-commit hooks for secret detection, linting, and testing. to enable them, run this once:

```bash
git config core.hooksPath .githooks
```

this configures git to automatically use hooks from `.githooks/` - no setup script needed!

## Testing

orla comes with extensive tests which can be run using

```bash
make test
```

For integration tests, use:

```bash
make test-integration
```

For end to end tests, use

```bash
make test-e2e
```

## Community + Contributions

Orla is built for the community. Contributions are not just welcome—they are essential. Whether it's reporting a bug, suggesting a feature, or writing code, we'd love your help. 

1. [Report a bug or request a feature](https://github.com/dorcha-inc/orla/issues)
2. Join us on [Discord](https://discord.gg/bzKYCFewPT).
3. Check out our [CONTRIBUTING.md](CONTRIBUTING.md) to get started.

All contributors are recognized in our [CONTRIBUTORS.md](CONTRIBUTORS.md) file.

## Supporting Orla

If Orla becomes a tool you love, please consider [sponsoring the project](https://github.com/sponsors/jadidbourbaki). Your support helps us dedicate more time to maintenance and building the future of local AI.

## Roadmap

See the RFCs in `docs/rfcs/` for the full vision and roadmap.

## Integration guides

- [Claude Desktop Integration](docs/integrations/claude-desktop.md)
- [MCP Client for Ollama Integration](docs/integrations/mcp-client-ollama.md)
- [Goose AI Agent Integration](docs/integrations/goose.md)