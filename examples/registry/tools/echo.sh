#!/bin/bash
# echo - Echo a message back to the user

message=""

# Parse arguments (orla passes --key value pairs)
while [[ $# -gt 0 ]]; do
    case $1 in
    --message)
        message="$2"
        shift 2
        ;;
    --message=*)
        message="${1#--message=}"
        shift
        ;;
    *)
        shift
        ;;
    esac
done

if [[ -z "$message" ]]; then
    echo "Error: --message is required" >&2
    exit 1
fi

echo "$message"
exit 0
