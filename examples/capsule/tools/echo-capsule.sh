#!/bin/sh
# echo-capsule - A capsule mode tool that echoes input

# Send handshake notification immediately
echo '{"jsonrpc":"2.0","method":"orla.hello","params":{"name":"echo-capsule","version":"1.0.0","capabilities":["tools"]}}'

# Read and respond to JSON-RPC requests
while IFS= read -r line; do
    # Extract request ID using sed
    REQ_ID=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
    if [ -n "$REQ_ID" ]; then
        # Extract arguments from the request
        # For simplicity, we'll just echo back a formatted response
        # In a real tool, you'd parse the arguments and do actual work
        echo "{\"jsonrpc\":\"2.0\",\"id\":$REQ_ID,\"result\":{\"echoed\":\"Capsule mode echo tool received your request with ID $REQ_ID\"}}"
    fi
done
