#!/bin/sh
# Uninstall script for Orla
# This script removes Orla only. Ollama and models are left intact.

set -eu

status() { echo "STATUS: $*" >&2; }
success() { echo "SUCCESS: $*" >&2; }
warning() { echo "WARNING: $*" >&2; }
available() { command -v "$1" >/dev/null 2>&1; }

# Remove orla binary
remove_orla_binary() {
    status "removing orla binary..."

    # Get the path where orla is currently found (if any) and try removing it up to 3 times
    # This handles cases where there are multiple copies of orla in PATH
    MAX_ATTEMPTS=3
    ATTEMPT=1
    while [ $ATTEMPT -le $MAX_ATTEMPTS ]; do
        ORLA_PATH=""
        if available orla; then
            ORLA_PATH=$(command -v orla)
            # Remove it if it exists and is a file (not a symlink to a non-existent file)
            if [ -n "$ORLA_PATH" ] && [ -f "$ORLA_PATH" ]; then
                rm -f "$ORLA_PATH"
                success "removed $ORLA_PATH (attempt $ATTEMPT)"
            fi
            # If orla is still available after removal, try again
            if available orla && [ $ATTEMPT -lt $MAX_ATTEMPTS ]; then
                warning "orla is still in PATH, trying again (attempt $((ATTEMPT + 1))/$MAX_ATTEMPTS)..."
                ATTEMPT=$((ATTEMPT + 1))
                # Small delay to allow filesystem to update
                sleep 0.1
            else
                break
            fi
        else
            # orla is not in PATH, we're done
            break
        fi
    done

    # Remove from all common install locations
    for path in /usr/local/bin/orla /usr/bin/orla "$HOME/.local/bin/orla" "$HOME/bin/orla"; do
        if [ -f "$path" ]; then
            rm -f "$path"
            success "removed $path"
        fi
    done
}

# Main uninstall process
main() {
    status "uninstalling orla..."

    remove_orla_binary

    success "orla uninstalled successfully!"
    echo ""
    echo "Note: ollama and models were not removed."
    if available brew; then
        echo "To remove Ollama: brew uninstall ollama"
    else
        echo "To remove Ollama, visit: https://ollama.ai or check your system's package manager"
    fi
}

main
