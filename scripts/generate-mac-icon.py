#!/usr/bin/env python3
"""Build daemon/macapp/Mewla.icns, the macOS app icon, from the Mewla app icon.

Run from anywhere after changing app/assets/branding/mewla-icon.png:
    python3 scripts/generate-mac-icon.py
"""
from pathlib import Path

from PIL import Image

root = Path(__file__).resolve().parent.parent
source = root / "app/assets/branding/mewla-icon.png"
target = root / "daemon/macapp/Mewla.icns"

image = Image.open(source).convert("RGBA")
image.save(target, format="ICNS", sizes=[(16, 16), (32, 32), (64, 64), (128, 128), (256, 256), (512, 512), (1024, 1024)])
print(f"{target.relative_to(root)}: {target.stat().st_size} bytes")
