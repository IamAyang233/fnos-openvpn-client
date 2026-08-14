#!/usr/bin/env python3
"""Regenerate the openvpn-client icon (ICON.PNG 64x64 + ICON_256.PNG 256x256).

Design language (cohesive with openvpn-as server icon):
  - Blue rounded-square tile, vertical gradient (OpenVPN blue family).
  - White SHIELD silhouette (privacy/security) -- same family as VPN brand.
  - Upward ARROW knocked out of the shield (client INITIATES the outbound
    secure tunnel) -- visually distinct from the server's central mark.

Rendered at 4x (1024) supersample then downscaled with LANCZOS for clean edges.
"""
import numpy as np
from PIL import Image, ImageDraw

S = 1024  # supersample size
CX = CY = S // 2

# ---- gradient colours (OpenVPN blue family) ----
TOP = (46, 134, 246)    # vivid blue (top)
BOT = (18, 84, 196)     # deeper blue (bottom)

def lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))

def rounded_tile():
    """Return a (S,S,4) uint8 array: blue rounded-rect with vertical gradient + soft top sheen."""
    img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    radius = int(S * 0.22)
    # gradient fill
    for y in range(S):
        t = y / (S - 1)
        col = lerp(TOP, BOT, t)
        d.line([(0, y), (S, y)], fill=col)
    # rounded corners: erase outside corners by drawing transparent mask then intersect
    mask = Image.new("L", (S, S), 0)
    md = ImageDraw.Draw(mask)
    md.rounded_rectangle([0, 0, S - 1, S - 1], radius=radius, fill=255)
    out = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    out.paste(img, (0, 0), mask)
    # soft top sheen
    sheen = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    sd = ImageDraw.Draw(sheen)
    sr = int(S * 0.22)
    sd.rounded_rectangle([0, 0, S - 1, int(S * 0.42)], radius=sr, fill=(255, 255, 255, 38))
    out = Image.alpha_composite(out, sheen)
    return np.array(out, dtype=np.float64)

def shield_points():
    w = int(S * 0.30)        # half width
    h = int(S * 0.285)       # half height (top)
    top = CY - h
    tip = CY + int(h * 1.18) # bottom point
    side_bottom = CY + int(h * 0.38)
    pts = []
    pts.append((CX - w, top))        # top-left
    pts.append((CX + w, top))        # top-right
    pts.append((CX + w, side_bottom))  # right shoulder
    # quadratic bezier right shoulder -> tip (control below-right)
    c1 = (CX + w, tip)
    for k in range(1, 13):
        t = k / 12
        x = (1 - t) ** 2 * (CX + w) + 2 * (1 - t) * t * c1[0] + t ** 2 * CX
        y = (1 - t) ** 2 * side_bottom + 2 * (1 - t) * t * c1[1] + t ** 2 * tip
        pts.append((x, y))
    # quadratic bezier tip -> left shoulder (control below-left)
    c2 = (CX - w, tip)
    for k in range(1, 13):
        t = k / 12
        x = (1 - t) ** 2 * CX + 2 * (1 - t) * t * c2[0] + t ** 2 * (CX - w)
        y = (1 - t) ** 2 * tip + 2 * (1 - t) * t * c2[1] + t ** 2 * side_bottom
        pts.append((x, y))
    return pts

def arrow_points():
    sw = int(S * 0.085)   # stem half-width
    aw = int(S * 0.20)    # head half-width
    head_top = CY - int(S * 0.205)
    head_base = CY - int(S * 0.04)
    stem_bot = CY + int(S * 0.155)
    return [
        (CX - sw, stem_bot),
        (CX - sw, head_base),
        (CX - aw, head_base),
        (CX, head_top),
        (CX + aw, head_base),
        (CX + sw, head_base),
        (CX + sw, stem_bot),
    ]

def mask_from_polygon(pts):
    m = Image.new("L", (S, S), 0)
    ImageDraw.Draw(m).polygon(pts, fill=255)
    return np.array(m, dtype=np.float64) / 255.0

def build():
    base = rounded_tile()                       # (S,S,4) float
    shield = mask_from_polygon(shield_points())  # (S,S) 0..1
    arrow = mask_from_polygon(arrow_points())    # (S,S) 0..1

    shield_only = shield * (1.0 - arrow)         # white region
    # arrow region keeps base (knockout) automatically since shield_only=0 there

    sn = shield_only[..., None]
    white = np.array([255, 255, 255, 255], dtype=np.float64)
    out = base * (1.0 - sn) + white * sn
    out = np.clip(out, 0, 255).astype(np.uint8)
    return Image.fromarray(out, "RGBA")

def save_all():
    hi = build()                                  # 1024
    hi256 = hi.resize((256, 256), Image.LANCZOS)
    hi64 = hi.resize((64, 64), Image.LANCZOS)
    hi256.save("fnos/ICON_256.PNG", "PNG")
    hi64.save("fnos/ICON.PNG", "PNG")
    # arm64 copy identical
    hi256.save("fnos_arm64_v4/ICON_256.PNG", "PNG")
    hi64.save("fnos_arm64_v4/ICON.PNG", "PNG")
    print("saved 64 + 256 (x86 + arm64)")

if __name__ == "__main__":
    save_all()
