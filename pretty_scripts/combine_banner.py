#!/usr/bin/env python3
"""
Combine orla_cat.txt and orla_logo.txt side by side, aligning their bottoms.
"""

import re
from pathlib import Path

# Get the script directory and project root
SCRIPT_DIR = Path(__file__).parent
PROJECT_ROOT = SCRIPT_DIR.parent
SHARE_DIR = PROJECT_ROOT / "share"


def get_display_width(line):
    """Get the display width of a line, ignoring ANSI escape sequences."""
    ansi_escape = re.compile(r"\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")
    return len(ansi_escape.sub("", line.rstrip()))


def combine_banner():
    """Combine cat and logo files, aligning bottoms."""
    cat_file = SHARE_DIR / "orla_cat.txt"
    logo_file = SHARE_DIR / "orla_logo.txt"
    output_file = SHARE_DIR / "orla_banner.txt"

    # Read both files
    with open(cat_file, "r") as f:
        cat_lines = [line.rstrip("\n") for line in f.readlines()]

    with open(logo_file, "r") as f:
        logo_lines = [line.rstrip("\n") for line in f.readlines()]

    # Find the maximum width of cat lines (for padding)
    cat_width = max(get_display_width(line) for line in cat_lines) if cat_lines else 0

    # Align bottoms: pad cat at the top
    cat_len = len(cat_lines)
    logo_len = len(logo_lines)

    if cat_len < logo_len:
        # Pad cat at the top with empty lines
        padding = [""] * (logo_len - cat_len)
        cat_lines = padding + cat_lines
    elif logo_len < cat_len:
        # Pad logo at the top with empty lines
        padding = [""] * (cat_len - logo_len)
        logo_lines = padding + logo_lines

    # Combine line by line
    combined = []
    for i in range(len(cat_lines)):
        cat_line = cat_lines[i]
        logo_line = logo_lines[i]

        # Pad cat line to its max width
        cat_display_width = get_display_width(cat_line)
        padding = (
            " " * (cat_width - cat_display_width)
            if cat_display_width < cat_width
            else ""
        )

        combined_line = cat_line + padding + "  " + logo_line
        combined.append(combined_line)

    # Write to output file
    with open(output_file, "w") as f:
        f.write("\n".join(combined))
        f.write("\n")

    print(f"Created {output_file}")
    print(f"Cat lines: {cat_len}, Logo lines: {logo_len}, Combined: {len(combined)}")


if __name__ == "__main__":
    combine_banner()
