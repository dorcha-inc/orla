#!/bin/sh
# Test script for verifying orla works with a remote ollama instance
# This script sets up a Docker ollama container and tests orla connectivity
# Usage:
#   ./test-remote-ollama.sh [env|config]
#   - env: Test using OLLAMA_HOST environment variable (default)
#   - config: Test using llm_backend configuration in orla.yaml

set -eu

TEST_MODE="${1:-env}"
MODEL_NAME="${2:-qwen3:0.6b}"

echo "testing orla with remote ollama (mode: $TEST_MODE)"

# Start ollama in Docker to simulate a remote ollama server
echo "starting ollama container..."
docker run -d --name ollama-test -p 11434:11434 ollama/ollama:latest

# Cleanup function
cleanup() {
    echo "cleaning up ollama container..."
    docker stop ollama-test 2>/dev/null || true
    docker rm ollama-test 2>/dev/null || true
}
trap cleanup EXIT

# Wait for ollama to be ready
echo "waiting for ollama API to be ready..."
max_attempts=60
attempt=0
while [ $attempt -lt $max_attempts ]; do
    if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        echo "ollama API is ready"
        break
    fi
    sleep 1
    attempt=$((attempt + 1))
done

if [ $attempt -eq $max_attempts ]; then
    echo "ERROR: ollama API not ready after 60 seconds" >&2
    exit 1
fi

# Pull the model in the container
echo "pulling model $MODEL_NAME in container..."
docker exec ollama-test ollama pull "$MODEL_NAME"

# Wait for model to be ready
echo "waiting for model to be ready..."
sleep 5

# Test based on mode
if [ "$TEST_MODE" = "config" ]; then
    # ensure environment variables are not set to avoid interference with the test
    # (llm_backend config takes precedence, but we unset env vars for clarity)
    unset OLLAMA_HOST
    unset ORLA_OLLAMA_HOST

    # test using llm_backend configuration
    echo "testing with llm_backend configuration..."

    # create orla.yaml with llm_backend config
    cat >orla.yaml <<EOF
llm_backend:
  endpoint: http://localhost:11434
  type: ollama
EOF

    # test orla agent with config
    orla agent "hello world" || (echo "ERROR: orla agent failed with llm_backend config" >&2 && exit 1)

    # clean up config file
    rm -f orla.yaml
else
    # ensure llm_backend config is not set to avoid interference with the test
    rm -f orla.yaml

    # ensure OLLAMA_HOST is not set to avoid interference with the test
    unset OLLAMA_HOST
    # Test using ORLA_OLLAMA_HOST environment variable
    echo "testing with ORLA_OLLAMA_HOST environment variable..."
    export ORLA_OLLAMA_HOST=http://localhost:11434

    # test orla agent with env var
    orla agent "hello world" || (echo "ERROR: orla agent failed with ORLA_OLLAMA_HOST" >&2 && exit 1)
fi

echo "remote ollama test passed: orla works with docker ollama (mode: $TEST_MODE)"
