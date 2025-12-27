# Using Orla with Goose

This guide will help you integrate Orla with [Goose](https://github.com/block/goose), an open source, extensible AI agent.

## Prerequisites

- [Goose CLI](https://github.com/block/goose) installed
- Orla installed (see [Installation](#installation))
- At least one tool installed via Orla
- An LLM configured in Goose (supports any LLM with tool calling capabilities)

Here is a helpful [guide](https://block.github.io/goose/blog/2025/03/14/goose-ollama/) to setting up Goose completely locally with Ollama.

## Installation

### Install Orla

First, install Orla:

```bash
go install github.com/dorcha-inc/orla/cmd/orla@latest
```

Or build from source:

```bash
git clone https://github.com/dorcha-inc/orla.git
cd orla
make install
```

Verify installation:

```bash
orla --version
```

## Install Tools

Install some useful tools from Orla's registry. For example, install the file system operations tool:

```bash
orla install fs
```

Or install other tools:

```bash
orla install coinflip
```

## Configure Goose to Use Orla

Goose CLI supports MCP servers through extensions. You can configure Orla as an extension using the interactive configuration wizard.

### Find Orla Path

First, find where Orla is installed:

```bash
which orla
```

Save this path as `<ORLA_PATH>` for the configuration below.

### Configure Orla as an extension Using Goose CLI

Use the Goose CLI configuration wizard to add Orla as an extension:

1. Run the Configuration Wizard:

   ```bash
   goose configure
   ```

2. Select `Add Extension` from the menu

3. Select `Command-line Extension`

4. Enter Extension Details:
   - Extension Name: `orla`
   - Command: The full path to the `orla` binary (from `which orla`)
   - Arguments: `serve --stdio`
   - Timeout: `300` (or your preferred timeout in seconds)

The CLI will save the configuration automatically. Goose will now connect to Orla as an MCP server and discover all available tools.

### Manual Configuration File

If you prefer to edit the configuration file directly, Goose CLI stores its configuration at:

- **macOS/Linux**: `~/.config/goose/config.yaml`
- **Windows**: `%APPDATA%\Block\goose\config\config.yaml`

**Note**: The exact format may vary by Goose version. Using `goose configure` is recommended as it ensures the correct format for your version.

If you need to edit manually, refer to the [Goose Configuration Files documentation](https://block.github.io/goose/docs/guides/config-files/) for the exact format. The configuration typically includes an `extensions` section, but the structure may differ.

## Using Orla Tools with Goose

Once configured, Goose will automatically discover and use all tools available through Orla. Here are some example use cases:

## Related Documentation

- [Goose GitHub Repository](https://github.com/block/goose)
- [Goose Documentation](https://block.github.io/goose/)
- [Orla README](../../README.md)
- [Claude Desktop Integration](./claude-desktop.md)
- [MCP Client for Ollama Integration](./mcp-client-ollama.md)

## Getting Help

If you encounter issues: Ask for help in our [Discord](https://discord.gg/bzKYCFewPT) or open a [GitHub issue](https://github.com/dorcha-inc/orla/issues)
