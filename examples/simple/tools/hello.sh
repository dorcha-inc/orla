#!/bin/bash
# hello - A simple greeting tool

name="World"

# Parse arguments (orla passes --key value pairs)
while [[ $# -gt 0 ]]; do
    case $1 in
    --name)
        name="$2"
        shift 2
        ;;
    --name=*)
        name="${1#--name=}"
        shift
        ;;
    *)
        shift
        ;;
    esac
done

echo "Hello, $name!"
exit 0
