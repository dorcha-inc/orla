# orla-test

A CLI tool for testing orla MCP tools via HTTP or stdio transports.

## installation

Build with the main project:

```bash
make build
```

This creates `bin/orla-test`. You can also install it globally:

```bash
make install-test
# or
go install ./cmd/orla-test
```

## usage

### Initialize Session (HTTP only)

```bash
orla-test init --port 8080
```

### Call a Tool

```bash
# HTTP transport
orla-test call --transport http --port 8080 --tool hello --args '{"name":"World"}'

# With stdin
orla-test call --transport http --port 8080 --tool greet --args '{"language":"es"}' --stdin "María"

# stdio transport
orla-test call --transport stdio --tool hello --args '{"name":"World"}'

# Short flags
orla-test call -t http -p 8080 -n hello -a '{"name":"World"}'
```

## commands

### `init`

Initialize an MCP session (HTTP transport only).

**Flags:**
- `-p, --port`: HTTP port (default: 8080)

### `call`

Call an MCP tool and display its output.

**Flags:**
- `-t, --transport`: Transport to use (`http` or `stdio`, default: `http`)
- `-p, --port`: HTTP port (default: 8080, ignored for stdio)
- `-n, --tool`: Tool name to call (required)
- `-a, --args`: Tool arguments as JSON (default: `{}`)
- `-s, --stdin`: Stdin input for tool (optional)
- `--orla-bin`: Path to orla binary (for stdio transport, default: auto-detect)

## examples

See the example Makefiles in `examples/` for usage patterns.

## help

Get help for any command:

```bash
orla-test --help
orla-test init --help
orla-test call --help
```
