# capsule example

This example demonstrates capsule mode tools - long-running tools that maintain state and can handle multiple requests efficiently.

## What is Capsule Mode?

Capsule mode tools are long-running processes that:
1. Start once when orla starts
2. Send a handshake notification (`orla.hello`) on startup
3. Communicate via JSON-RPC over stdin/stdout
4. Can handle multiple tool calls without restarting

This is useful for tools that:
- Need to maintain state between calls
- Have expensive initialization
- Benefit from connection pooling or caching
- Need to handle streaming or long-running operations

## Setup

1. Build orla (if you haven't already):
   ```bash
   cd ../..
   make build
   ```

2. Start orla:
   ```bash
   make run
   ```

   Or manually:
   ```bash
   ../../bin/orla serve --config orla.yaml
   ```

3. Test the capsule tool:
   ```bash
   make test
   ```

## How It Works

The `echo-capsule.sh` tool:
1. Sends an `orla.hello` notification on startup to signal it's ready
2. Reads JSON-RPC requests from stdin
3. Responds with JSON-RPC responses containing the result

The `orla.yaml` config file specifies the tool in `tools_registry` with:
- `runtime.mode: capsule` - enables capsule mode
- `runtime.startup_timeout_ms: 5000` - timeout for handshake

Note: This example uses `tools_registry` in the config file to explicitly define the tool with capsule mode. For installed tools, you would use a `tool.yaml` manifest instead.

## Notes

- Capsule tools must send the `orla.hello` notification within the startup timeout
- If a capsule fails to start, it won't be registered with the MCP server
- Capsule tools are stopped when orla shuts down
- Each tool call is sent as a JSON-RPC `tools/call` request

