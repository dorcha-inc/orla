# Using Orla with Claude Desktop

This guide will help you integrate orla with Claude Desktop, allowing Claude to use your custom tools. This guide follows the [official MCP documentation](https://modelcontextprotocol.io/docs/develop/connect-local-servers) for connecting local MCP servers to Claude Desktop.

## Prerequisites

- [Claude Desktop](https://claude.ai/download) installed
- orla installed (see [Installation](#installation))
- At least one executable tool file

## Installation

First, install orla:

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

## Install a tool

As an example, you can install a simple coinflip tool orla has in its registry for testing:

```bash
orla install coinflip
```

## Configure Claude Desktop

On Claude Desktop, go to `Settings > Developer` and click `Edit Config`.

<img src="share/claude-developer-settings.png" width="600"></img>

This opens the configuration file. Add orla to the `mcpServers` section.

Find where orla is installed:

```bash
which orla
```

Then use that path (`ORLA_PATH`) to fill out the Claude Desktop config.

```json
{
  "mcpServers": {
    "orla": {
      "command": "<ORLA_PATH>",
      "args": ["serve", "--stdio"]
    }
  }
}
```

## Restart Claude Desktop

After saving the configuration file, completely quit Claude Desktop and restart it. The application needs to restart to load the new configuration and start the MCP server.

## Verifying the Integration

Upon successful restart, click the `+` button below your conversation in Claude and you should see orla in `Connectors`:

<img src="share/claude-desktop-orla.png" width="600"></img>

You can also try using the tool directly:

<img src="share/claude-orla-coinflip.png" width="600"></img>

<img src="share/claude-orla-coinflip-tails.png" width="600"></img>

## Getting Help

If you encounter issues, please feel free to ask for help in our [discord](https://discord.gg/QawsSFnR) or 
open a [github issue](https://github.com/dorcha-inc/orla/issues).


## Related Documentation

- [README.md](../../README.md)
- [Examples](../../examples/)
- [RFC 1](../rfcs/rfc1.txt)
- [RFC 3](../rfcs/rfc3.txt)

