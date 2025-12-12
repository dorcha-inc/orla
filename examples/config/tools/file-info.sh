#!/bin/bash
# file-info - Display file information

format="text"
include_size=false
path=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
    --path)
        path="$2"
        shift 2
        ;;
    --path=*)
        path="${1#--path=}"
        shift
        ;;
    --format)
        format="$2"
        shift 2
        ;;
    --format=*)
        format="${1#--format=}"
        shift
        ;;
    --include-size)
        # orla passes --include-size <value>, so check the value
        if [[ "$2" == "true" ]] || [[ "$2" == "1" ]] || [[ -z "$2" ]]; then
            include_size=true
        fi
        # Always shift past the value (or just the flag if no value)
        if [[ -n "$2" ]]; then
            shift 2
        else
            shift
        fi
        ;;
    *)
        echo "Unknown argument: $1" >&2
        exit 1
        ;;
    esac
done

# Validate required arguments
if [[ -z "$path" ]]; then
    echo "Error: --path is required" >&2
    exit 1
fi

# Validate file exists
if [[ ! -e "$path" ]]; then
    echo "Error: File not found: $path" >&2
    exit 1
fi

# Collect file information
info=()
info+=("path: $path")

if [[ -f "$path" ]]; then
    info+=("type: file")
elif [[ -d "$path" ]]; then
    info+=("type: directory")
else
    info+=("type: other")
fi

if [[ "$include_size" == "true" ]]; then
    if [[ -f "$path" ]]; then
        size=$(stat -f%z "$path" 2>/dev/null || stat -c%s "$path" 2>/dev/null || echo "unknown")
        info+=("size: $size bytes")
    fi
fi

info+=("readable: $([ -r "$path" ] && echo "yes" || echo "no")")
info+=("writable: $([ -w "$path" ] && echo "yes" || echo "no")")
info+=("executable: $([ -x "$path" ] && echo "yes" || echo "no")")

# Output in requested format
if [[ "$format" == "json" ]]; then
    echo "{"
    for i in "${!info[@]}"; do
        key="${info[$i]%%: *}"
        value="${info[$i]#*: }"
        echo -n "  \"$key\": \"$value\""
        if [[ $i -lt $((${#info[@]} - 1)) ]]; then
            echo ","
        else
            echo
        fi
    done
    echo "}"
else
    for item in "${info[@]}"; do
        echo "$item"
    done
fi

exit 0
