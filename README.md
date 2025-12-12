# orla

<img src="share/orla_gemini_upscaled.png" alt="orla logo" width="128">

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/dorcha-inc/orla)](https://goreportcard.com/report/github.com/dorcha-inc/orla)
[![Test](https://github.com/dorcha-inc/orla/actions/workflows/test.yml/badge.svg)](https://github.com/dorcha-inc/orla/actions/workflows/test.yml)
[![Lint](https://github.com/dorcha-inc/orla/actions/workflows/lint.yml/badge.svg)](https://github.com/dorcha-inc/orla/actions/workflows/lint.yml)
[![Build](https://github.com/dorcha-inc/orla/actions/workflows/build.yml/badge.svg)](https://github.com/dorcha-inc/orla/actions/workflows/build.yml)
[![Coverage](https://codecov.io/gh/dorcha-inc/orla/branch/main/graph/badge.svg)](https://codecov.io/gh/dorcha-inc/orla)

orla is a runtime for model context protocol ([MCP](https://modelcontextprotocol.io/docs/getting-started/intro)) servers that automatically discovers and executes tools from the filesystem. Just drop executable files in a `tools/` directory and orla makes them available as MCP tools! No configuration required.

> To see all the amazing folks who have taken their time to contribute something cool to orla, please take a look at [CONTRIBUTORS.md](CONTRIBUTORS.md).

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
orla
```

this runs on port `8080` by default.

You can run it using `stdio` as the transport:

```bash
orla --stdio
```

You can specify a custom port

```bash
orla --port 3000
```

You can also specify a custom configuration file

```bash
orla --config orla.json
```

3. You can hot reload orla, i.e., get it to refresh its tools and configuration without restarting.

```bash
kill -HUP $(pgrep orla)
```

## configuration

orla works out of the box with zero configuration, but you can customize it with a JSON config file:

```json
{
  "tools_dir": "./tools",
  "port": 8080,
  "timeout": 30,
  "log_format": "json",
  "log_level": "info"
}
```

The configuration options for orla are as follows

- `tools_dir`: Directory containing executable tools (default: `./tools`)
- `port`: HTTP server port (default: `8080`, ignored in stdio mode)
- `timeout`: Tool execution timeout in seconds (default: `30`)
- `log_format`: `"json"` or `"pretty"` (default: `"json"`)
- `log_level`: `"debug"`, `"info"`, `"warn"`, `"error"`, or `"fatal"` (default: `"info"`)

## command Line Options

```bash
orla [options]

Options:
  -config string    Path to orla.json config file
  -port int         Port to listen on (ignored if stdio is used, default: 8080)
  -stdio            Use stdio instead of TCP port
  -pretty           Use pretty-printed logs instead of JSON
```

## development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and contribution guidelines.

### git hooks

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

### roadmap

See the RFCs in `docs/rfcs/` for the full vision and roadmap.

### contributing

Thank you so much for considering contributing to orla! orla is an open-source project and runs on individual contributions from amazing people around the world. Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details.
