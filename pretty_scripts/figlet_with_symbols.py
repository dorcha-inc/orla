#!/usr/bin/env python3
"""
Convert figlet output to use custom symbols and colors.
Usage: python3 figlet_with_symbols.py [font] [text] [symbol_set] [color]
"""

import argparse
import sys
import subprocess

# Define symbol sets
SYMBOL_SETS = {
    "blocks": {"main": "█", "light": "▓", "medium": "▒", "dark": "░"},
    "unicode": {"main": "■", "light": "□", "medium": "▪", "dark": "▫"},
    "dots": {"main": "●", "light": "○", "medium": "▪", "dark": "·"},
    "stars": {"main": "★", "light": "☆", "medium": "✦", "dark": "✧"},
    "none": {"main": "N/A", "light": "N/A", "medium": "N/A", "dark": "N/A"},
}

# Color definitions (RGB values)
COLORS = {
    "irish_green": (0, 154, 0),
    "green": (0, 255, 0),
    "red": (255, 0, 0),
    "blue": (0, 0, 255),
    "yellow": (255, 255, 0),
    "cyan": (0, 255, 255),
    "magenta": (255, 0, 255),
    "white": (255, 255, 255),
    "orange": (255, 165, 0),
    "purple": (128, 0, 128),
    "pink": (255, 192, 203),
    "none": None,  # No color
}


def rgb_to_ansi(r, g, b):
    """Convert RGB to ANSI truecolor escape code."""
    return f"\033[38;2;{r};{g};{b}m"


def apply_color(text, color_name):
    """Apply color to text if color is specified."""
    if color_name == "none" or color_name not in COLORS:
        return text

    rgb = COLORS[color_name]
    if rgb is None:
        return text

    color_code = rgb_to_ansi(*rgb)
    reset = "\033[0m"

    # Apply color to each line (but preserve ANSI codes if any)
    lines = text.split("\n")
    colored_lines = []
    for line in lines:
        if line.strip():
            colored_lines.append(f"{color_code}{line}{reset}")
        else:
            colored_lines.append(line)

    return "\n".join(colored_lines)


def replace_with_symbols(text, symbol_set):
    """Replace characters in text with symbols."""
    symbols = SYMBOL_SETS.get(symbol_set, SYMBOL_SETS["blocks"])

    result = []
    for line in text:
        new_line = ""
        for char in line:
            if char.isalnum():
                # Letters and numbers -> main symbol
                new_line += symbols["main"]
            elif char in ".,;:!?":
                # Punctuation -> dark symbol
                new_line += symbols["dark"]
            elif char in "\"'*^%=+@><":
                # Special chars -> medium symbol
                new_line += symbols["medium"]
            else:
                # Keep spaces and newlines
                new_line += char
        result.append(new_line)

    return result


def main():
    parser = argparse.ArgumentParser(
        description="Convert figlet output to use custom symbols and colors.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s fraktur orla blocks irish_green
  %(prog)s nvscript orla stars red
  %(prog)s big orla --no-symbol-replace blue
  %(prog)s script orla blocks --no-color
        """,
    )

    parser.add_argument(
        "font",
        nargs="?",
        default="fraktur",
        help="Figlet font to use (default: fraktur)",
    )
    parser.add_argument(
        "text",
        nargs="?",
        default="Orla",
        help="Text to display (default: Orla)",
    )
    parser.add_argument(
        "symbol_set",
        nargs="?",
        default="blocks",
        choices=list(SYMBOL_SETS.keys()),
        help="Symbol set to use",
    )
    parser.add_argument(
        "color",
        nargs="?",
        default="none",
        choices=list(COLORS.keys()),
        help="Color to apply",
    )
    parser.add_argument(
        "--no-symbol-replace",
        action="store_true",
        help="Skip symbol replacement, use original figlet characters",
    )
    parser.add_argument(
        "--no-color",
        action="store_true",
        help="Skip color application, output plain text",
    )

    args = parser.parse_args()

    # Generate figlet output
    try:
        result = subprocess.run(
            ["figlet", "-f", args.font, args.text],
            capture_output=True,
            text=True,
            check=True,
        )
        figlet_output = result.stdout
    except subprocess.CalledProcessError:
        print(
            f"Error: Could not generate figlet output with font '{args.font}'",
            file=sys.stderr,
        )
        sys.exit(1)
    except FileNotFoundError:
        print("Error: figlet not found. Please install figlet.", file=sys.stderr)
        sys.exit(1)

    # Replace with symbols (unless disabled)
    if args.no_symbol_replace or args.symbol_set == "none":
        output = figlet_output
    else:
        lines = figlet_output.split("\n")
        symbol_lines = replace_with_symbols(lines, args.symbol_set)
        output = "\n".join(symbol_lines)

    # Apply color (unless disabled)
    if args.no_color or args.color == "none":
        final_output = output
    else:
        final_output = apply_color(output, args.color)

    print(final_output, end="")


if __name__ == "__main__":
    main()
