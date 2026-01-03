#!/bin/sh
# Uninstall script for Orla
# This script removes Orla only. Ollama and models are left intact.

set -eu

status() { echo "STATUS: $*" >&2; }
success() { echo "SUCCESS: $*" >&2; }
warning() { echo "WARNING: $*" >&2; }
available() { command -v "$1" >/dev/null 2>&1; }

keep_removing_orla_if_in_path() {
    ORLA_PATH=""
    if available orla; then
        ORLA_PATH=$(command -v orla)
    fi

    rm -f "$ORLA_PATH"
    success "removed $ORLA_PATH"

    if available orla; then
        warning "orla is still in the PATH, removing it again..."
        keep_removing_orla_if_in_path
    fi
}

# Remove orla binary
remove_orla_binary() {
    status "removing orla binary..."

    keep_removing_orla_if_in_path

    # check common install locations
    ORLA_PATHS=""
    # if the path exists, add it to the list
    for path in /usr/local/bin/orla /usr/bin/orla "$HOME/.local/bin/orla" "$HOME/bin/orla"; do
        if [ -f "$path" ]; then ORLA_PATHS="$ORLA_PATHS $path"; fi
    done

    for path in $ORLA_PATHS; do
        rm -f "$path"
        success "removed $path"
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
