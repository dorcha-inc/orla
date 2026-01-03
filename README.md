<div align="center">
  <a href="https://github.com/dorcha-inc/orla">
    <img src="share/orla_canva.png" alt="Orla Logo" width="400">
  </a>
  <br>
  <h3 align="center">A dead-simple unix tool for lightweight agents</h3>
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

<p align="center">
  <img src="share/main.gif" alt="Orla Demo" width="800">
</p>

Orla is a unix tool for running lightweight open-source agents. It is easy to add to a script, use with pipes, or build things on top of.

## Quickstart

Install via Homebrew on MacOS or Linux:

```bash
brew install --cask dorcha-inc/orla/orla
```

or install orla via a helper script (you might need `sudo`):

```bash
curl -fsSL https://raw.githubusercontent.com/dorcha-inc/orla/main/scripts/install.sh | sh
```

Try orla:

```bash
>>> orla agent "Hello"
Hello! How can I assist you today? Could you please provide some details or specify what you need help with?
```

All done!

Side note: if required, this will install go, ollama, and pull in a lightweight open-source model.

## Vision and Roadmap

Simple and usable tools are a key part of the [Unix philosophy](https://en.wikipedia.org/wiki/Unix_philosophy). Tools like `grep`, `curl`, and `git` have become second nature and are huge wins for an inclusive and productive ecosystem. They are fast, reliable, and composable. However, the ecosystem around AI and AI agents currently feels like using a bloated monolithic piece of proprietary software with over-priced and kafkaesque licensing fees.

Orla is built on a simple premise: AI should be a (free software) tool you own, not a service you rent. It treats simplicity, reliability, and composability as first-order priorities. Orla uses models running on your own machine and automatically discovers the tools you already have, making it powerful and private right out of the box. It requires no API keys, subscriptions, or power-hungry data centers. To summarize,

1. Orla runs locally. Your data, queries, and tools never leave your machine without your explicit instruction. It's private by default.
2. Orla brings the power of modern LLMs to your terminal with a dead-simple interface. If you know how to use `grep`, you know how to use Orla.
3. Orla is free and open-source software. No subscriptions, no vendor lock-in. You are not the product.

See the RFCs in `docs/rfcs/` for more details on the roadmap.


<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
## Navigation

- [Getting Started](#getting-started)
  - [Installation](#installation)
  - [Usage](#usage)
    - [Use `orla agent` on a terminal directly](#use-orla-agent-on-a-terminal-directly)
    - [Use `orla serve` to integrate with other MCP clients](#use-orla-serve-to-integrate-with-other-mcp-clients)
    - [Installing Tools from the Registry](#installing-tools-from-the-registry)
    - [Creating Custom Tools](#creating-custom-tools)
- [Configuring Orla](#configuring-orla)
  - [Configuration Options](#configuration-options)
    - [MCP Server options](#mcp-server-options)
    - [Orla Agent options](#orla-agent-options)
  - [Example Configuration](#example-configuration)
- [Developer's Guide](#developers-guide)
  - [Building](#building)
  - [Git hooks](#git-hooks)
  - [Testing](#testing)
- [Community + Contributions](#community--contributions)
  - [Supporting Orla](#supporting-orla)
  - [Integration guides](#integration-guides)
- [Miscellaneous](#miscellaneous)
  - [Uninstalling Orla](#uninstalling-orla)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Getting Started

### Installation

The easiest and recommended way to install Orla on macOS and Linux is using [Homebrew](https://brew.sh/):

```bash
brew install --cask dorcha-inc/orla/orla
```

Alternatively, you can use our installation script. It will automatically install Orla, Ollama, and set everything up for you:

```bash
curl -fsSL https://raw.githubusercontent.com/dorcha-inc/orla/main/scripts/install.sh | sh
```

To remove orla, see [uninstalling orla](#uninstalling-orla).


### Usage

Orla supports two modes of operation: `agent` for direct terminal interaction, and `serve` for integration with MCP clients.

#### Use `orla agent` on a terminal directly

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

#### Use `orla serve` to integrate with other MCP clients

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

#### Installing Tools from the Registry

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

#### Creating Custom Tools

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

## Configuring Orla

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

## Developer's Guide

### Building

If you prefer to install manually, make sure you have Go (1.25+) installed, then:

```bash
go install github.com/dorcha-inc/orla/cmd/orla@latest
```

Or build it locally by cloning this repository and running:

```bash
make build
```

and then install locally:

```bash
make install
```

### Git hooks

orla includes pre-commit hooks for secret detection, linting, and testing. to enable them, run this once:

```bash
git config core.hooksPath .githooks
```

this configures git to automatically use hooks from `.githooks/` - no setup script needed!

### Testing

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

All the amazing folks who have taken their time to contribute something cool to orla are listed in [CONTRIBUTORS.md](CONTRIBUTORS.md).

### Supporting Orla

If Orla becomes a tool you love, please consider [sponsoring the project](https://github.com/sponsors/jadidbourbaki). Your support helps us dedicate more time to maintenance and building the future of local AI.

### Integration guides

- [Claude Desktop Integration](docs/integrations/claude-desktop.md)
- [MCP Client for Ollama Integration](docs/integrations/mcp-client-ollama.md)
- [Goose AI Agent Integration](docs/integrations/goose.md)

## Miscellaneous

### Uninstalling Orla

If installed via Homebrew:

```bash
brew uninstall --cask orla
```

If installed via install script:

```bash
curl -fsSL https://raw.githubusercontent.com/dorcha-inc/orla/main/scripts/uninstall.sh | sh
```

Note: The uninstall script only removes Orla. Ollama and models are left intact. To remove Ollama:
- If installed via Homebrew: `brew uninstall ollama`
- Otherwise: Visit https://ollama.ai or check your system's package manager