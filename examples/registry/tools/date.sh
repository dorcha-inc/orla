#!/bin/bash
# date - Get current date and time

format="iso8601"

# Parse arguments (orla passes --key value pairs)
while [[ $# -gt 0 ]]; do
    case $1 in
    --format)
        format="$2"
        shift 2
        ;;
    --format=*)
        format="${1#--format=}"
        shift
        ;;
    *)
        shift
        ;;
    esac
done

case "$format" in
iso8601)
    date -u +"%Y-%m-%dT%H:%M:%SZ"
    ;;
rfc822)
    date -u +"%a, %d %b %Y %H:%M:%S %Z"
    ;;
unix)
    date +%s
    ;;
human)
    date
    ;;
*)
    echo "Error: Unknown format: $format" >&2
    echo "Supported formats: iso8601, rfc822, unix, human" >&2
    exit 1
    ;;
esac

exit 0
