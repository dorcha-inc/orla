#!/usr/bin/env python3
"""
Convert Orla logo to ANSI-colored ASCII art.
Usage: python3 orla_to_ascii.py <image_path> [width]
"""

import pathlib
import sys
from PIL import Image


def rgb_to_ansi(r, g, b):
    """Convert RGB to ANSI 256-color code."""
    # Use 6x6x6 color cube (216 colors) + 16 standard colors = 232 colors
    if r == g == b and r < 8:
        return 16  # Black
    if r == g == b and r > 248:
        return 231  # White
    # Map to 6x6x6 cube (16-231)
    r6 = int((r / 255.0) * 5)
    g6 = int((g / 255.0) * 5)
    b6 = int((b / 255.0) * 5)
    return 16 + (r6 * 36) + (g6 * 6) + b6


def orla_to_ascii(image_path: pathlib.Path, width: int) -> str:
    """Convert Orla logo to ANSI-colored ASCII art.

    Args:
        width: Output width in characters
    """
    # Open image
    img = Image.open(image_path)
    aspect_ratio = img.height / img.width
    height = int(width * aspect_ratio * 0.5)  # 0.5 because chars are taller than wide

    resampling = Image.Resampling.NEAREST
    img = img.resize((width, height), resampling)

    # ASCII characters from darkest to lightest
    ascii_chars = "█"

    # Convert to RGBA to handle transparency
    if img.mode != "RGBA":
        img = img.convert("RGBA")

    output = []
    pixels = img.load()

    for y in range(height):
        line = ""
        for x in range(width):
            r, g, b, a = pixels[x, y]

            # If pixel is transparent or very transparent, use space
            if a < 128:  # Threshold for transparency (0-255, 128 = 50% opacity)
                char = " "
                ansi_code = 16  # Black color for transparent areas
            else:
                # Calculate luminance (perceived brightness) using standard formula
                # This matches human eye sensitivity better than simple average
                luminance = 0.299 * r + 0.587 * g + 0.114 * b
                # Darken the image to match original better
                # Multiply by a factor < 1.0 to make it darker (closer to original)
                darkened = (
                    luminance * 0.3
                )  # Adjust this value (0.2-0.9) to fine-tune darkness
                # Map to ASCII character
                # Clamp to ensure we don't exceed bounds
                char_idx = min(
                    int((darkened / 255.0) * (len(ascii_chars) - 1)),
                    len(ascii_chars) - 1,
                )
                char = ascii_chars[char_idx]
                # Get ANSI color code
                ansi_code = rgb_to_ansi(r, g, b)

            # Add colored character
            line += f"\033[38;5;{ansi_code}m{char}\033[0m"
        output.append(line)

    return "\n".join(output)


if __name__ == "__main__":
    if len(sys.argv) < 1:
        print(
            "Usage: python3 orla_to_ascii.py image_path [width]",
            file=sys.stderr,
        )
        sys.exit(1)

    image_path = sys.argv[1]
    width = int(sys.argv[2]) if len(sys.argv) > 2 else 40

    ascii_art = orla_to_ascii(image_path, width)
    print(ascii_art)
