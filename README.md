<div align="center">
  <img src="share/orla_canva.png"></img>
</div>

---
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/dorcha-inc/orla)](https://goreportcard.com/report/github.com/dorcha-inc/orla)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/6573/badge)](https://www.bestpractices.dev/projects/6573)[![Test](https://github.com/dorcha-inc/orla/actions/workflows/test.yml/badge.svg)](https://github.com/dorcha-inc/orla/actions/workflows/test.yml)
[![Build](https://github.com/dorcha-inc/orla/actions/workflows/build.yml/badge.svg)](https://github.com/dorcha-inc/orla/actions/workflows/build.yml)
[![Coverage](https://codecov.io/gh/dorcha-inc/orla/branch/main/graph/badge.svg)](https://codecov.io/gh/dorcha-inc/orla)
---

orla is a runtime for model context protocol ([MCP](https://modelcontextprotocol.io/docs/getting-started/intro)) servers that automatically discovers and executes tools from the filesystem. Just drop executable files in a `tools/` directory and orla makes them available as MCP tools! No configuration required.

All the amazing folks who have taken their time to contribute something cool to orla are listed in [CONTRIBUTORS.md](CONTRIBUTORS.md). If you find orla useful, please consider [sponsoring](https://github.com/sponsors/jadidbourbaki) the orla project. Your support helps maintain and improve orla for everyone. Thank you!

## quick links

- [Getting Started](#getting-started)
- [Usage](#usage)
- [Configuration](#configuration)
- [Command Line Options](#command-line-options)
- [Community + Contributions](#community--contributions)
- [Roadmap](#roadmap)
- [Integration Guides](#integration-guides)

## getting started

to install orla, you can either just run

```bash
go install github.com/dorcha-inc/orla/cmd/orla@latest
```

or build it locally by cloning this repository and running

```bash
make build
```

or installing locally by running

```bash
make install
```

## usage

The following is a simple example of using orla to create a set of MCP tools.

1. Create a tools directory with some tools. The tools can be any kind of executable.

```bash
mkdir tools
cat > tools/hello.sh << 'EOF'
#!/bin/bash
echo "Hello from orla!"
EOF
chmod +x tools/hello.sh
```

2. Start orla: 

```bash
orla serve
```

this runs on port `8080` by default.

You can run it using `stdio` as the transport:

```bash
orla serve --stdio
```

You can specify a custom port

```bash
orla serve --port 3000
```

You can also specify a custom configuration file:

```bash
orla serve --config orla.yaml
```

If no configuration file is specified, orla will automatically check for `orla.yaml` in the current directory. If not found, default configuration is used.

3. You can hot reload orla, i.e., get it to refresh its tools and configuration without restarting.

```bash
kill -HUP $(pgrep orla)
```

## configuration

orla works out of the box with zero configuration, but you can customize it with a YAML config file:

```yaml
tools_dir: ./tools
port: 8080
timeout: 30
log_format: json
log_level: info
```

The configuration options for orla are as follows

- `tools_dir`: Directory containing executable tools (default: `./tools`)
- `port`: HTTP server port (default: `8080`, ignored in stdio mode)
- `timeout`: Tool execution timeout in seconds (default: `30`)
- `log_format`: `"json"` or `"pretty"` (default: `"json"`)
- `log_level`: `"debug"`, `"info"`, `"warn"`, `"error"`, or `"fatal"` (default: `"info"`)

## command line options

```bash
orla serve [options]

Options:
  -config string    Path to orla.yaml config file
  -port int         Port to listen on (ignored if stdio is used, default: 8080)
  -stdio            Use stdio instead of TCP port
  -pretty           Use pretty-printed logs instead of JSON
  -tools-dir string Directory containing tools (overrides config file)
```

## git hooks

orla includes pre-commit hooks for secret detection, linting, and testing. to enable them, run this once:

```bash
git config core.hooksPath .githooks
```

this configures git to automatically use hooks from `.githooks/` - no setup script needed!

## testing

orla comes with extensive tests which can be run using

```bash
make test
```


## community + contributions

Contributions are very welcome! orla is an open-source project and runs on individual contributions from amazing people around the world. Contributions are welcome! For feature requests, bug reports, or usage problems, please feel free to create an issue. For more extensive contributions, check the [contribution guide](CONTRIBUTING.md). 

Join other orla users and developers on the following platforms:

[![Discord](https://img.shields.io/badge/Discord-5865F2?style=flat&logo=discord&logoColor=white)](https://discord.gg/QawsSFnR)
[![GitHub issues](https://img.shields.io/github/issues/dorcha-inc/orla)](https://github.com/dorcha-inc/orla/issues)

## roadmap

See the RFCs in `docs/rfcs/` for the full vision and roadmap.

## integration guides

- [Claude Desktop Integration](docs/integrations/claude-desktop.md)