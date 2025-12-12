#!/bin/bash
# greet - A greeting tool that processes stdin

language="en"

# Parse arguments (orla passes --key value pairs)
while [[ $# -gt 0 ]]; do
    case $1 in
    --language)
        language="$2"
        shift 2
        ;;
    --language=*)
        language="${1#--language=}"
        shift
        ;;
    *)
        shift
        ;;
    esac
done

# Read name from stdin
read -r name

if [[ -z "$name" ]]; then
    echo "Error: No name provided via stdin" >&2
    exit 1
fi

case "$language" in
en)
    echo "Hello, $name!"
    ;;
es)
    echo "¡Hola, $name!"
    ;;
fr)
    echo "Bonjour, $name!"
    ;;
de)
    echo "Hallo, $name!"
    ;;
*)
    echo "Hello, $name! (language '$language' not supported, using English)"
    ;;
esac

exit 0
