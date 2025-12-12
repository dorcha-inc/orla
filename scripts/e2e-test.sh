#!/bin/bash
# run end-to-end tests for all examples

set -euo pipefail

# Get the project root directory using git
PROJECT_ROOT="$(git rev-parse --show-toplevel)"
echo "project root: $PROJECT_ROOT"
cd "$PROJECT_ROOT"

# build binaries first
echo "building orla binaries"
make build

# track if any tests failed
FAILED=0

# test each example directory
for dir in examples/*/; do
    if [ ! -d "$dir" ]; then
        continue
    fi

    EXAMPLE_NAME=$(basename "$dir")
    echo ""
    echo "testing $EXAMPLE_NAME"

    cd "$dir"

    # start orla server in background
    echo "starting orla server in background"
    make run >"/tmp/orla-${EXAMPLE_NAME}.log" 2>&1 &
    ORLA_PID=$!

    echo "server started (PID: $ORLA_PID), waiting for it to be ready"
    sleep 0.1

    # check if server is still running
    if ! kill -0 "$ORLA_PID" 2>/dev/null; then
        echo "error: server failed to start. check /tmp/orla-${EXAMPLE_NAME}.log"
        cat "/tmp/orla-${EXAMPLE_NAME}.log"
        FAILED=1
        cd "$PROJECT_ROOT"
        continue
    fi

    # run tests
    echo "running tests"
    if ! make test; then
        echo "error: tests in $EXAMPLE_NAME failed"
        kill "$ORLA_PID" 2>/dev/null || true
        rm -f "/tmp/orla-${EXAMPLE_NAME}.log"
        FAILED=1
        cd "$PROJECT_ROOT"
        continue
    fi

    # stop server
    echo "stopping orla server (PID: $ORLA_PID)"
    kill "$ORLA_PID" 2>/dev/null || true
    wait "$ORLA_PID" 2>/dev/null || true
    rm -f "/tmp/orla-${EXAMPLE_NAME}.log"

    cd "$PROJECT_ROOT"
done

echo ""
if [ $FAILED -eq 0 ]; then
    echo "all e2e tests passed"
    exit 0
fi

echo "!!! some e2e tests failed !!!"
exit 1
