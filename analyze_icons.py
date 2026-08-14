#!/usr/bin/env python3
"""Render current client + server icons as downscaled ASCII grids to understand visual language."""
from PIL import Image

def grid(path, n=26):
    im = Image.open(path).convert("RGBA")
    w, h = im.size
    px = im.load()
    print(f"--- {path} ({w}x{h}) ---")
    for yi in range(n):
        row = ""
        for xi in range(n):
            x = int((xi + 0.5) / n * w)
            y = int((yi + 0.5) / n * h)
            r, g, b, a = px[x, y]
            if a < 60:
                row += "  "
                continue
            L = 0.299 * r + 0.587 * g + 0.114 * b
            mx, mn = max(r, g, b), min(r, g, b)
            if L < 40:
                row += "##"          # near-black
            elif mx < 90:
                row += ".."          # dark/gray
            elif r >= g >= b and (r - b) > 40 and r > 150:
                row += "RR"          # red/orange
            elif b >= g >= r and (b - r) > 40 and b > 150:
                row += "BB"          # blue
            elif g >= r and g >= b and (g - mn) > 40 and g > 150:
                row += "GG"          # green
            elif r > 180 and g > 180 and b > 180:
                row += "++"          # near-white
            else:
                row += "oo"          # mid tone
        print(row)
    print()

if __name__ == "__main__":
    grid("fnos/ICON_256.PNG", 24)
    grid("../openvpn-as-fpk/fnos/ICON_256.PNG", 24)
