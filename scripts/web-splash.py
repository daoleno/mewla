#!/usr/bin/env python3
"""Mewla's first screen in the web package's index.html, while the runtime downloads.

`nsh package --target web` writes a page that shows a small spinner after 0.4 s on a plain white or black
page until the app draws. Through a slow tunnel that first load can take minutes (owner 11-13). This makes
it Mewla's: the app's own background in both schemes (client/ui/theme.json), so nothing flashes from white
to the app's grey when it draws, and the default pet's still above the spinner, both appearing after the
same 0.4 s so a fast load shows nothing. It fails when the page is not the one it knows, so an NSH change
is seen, not silently skipped.

Usage: web-splash.py <package dir> <repo root>
"""
import glob
import json
import os
import sys

app, root = sys.argv[1], sys.argv[2]
page = os.path.join(app, "index.html")
html = open(page, encoding="utf-8").read()

theme = json.load(open(os.path.join(root, "client/ui/theme.json")))["cssVars"]
light, dark = theme["light"]["background"], theme["dark"]["background"]
pet = json.load(open(os.path.join(root, "client/core/pets/manifest.json")))["default"]
stills = glob.glob(os.path.join(app, "assets", "assets", "pets", pet, "idle-still.*.webp"))
if len(stills) != 1:
    sys.exit(f"web-splash: expected one {pet}/idle-still in the package, found {len(stills)}")
still = os.path.relpath(stills[0], app)

STATUS = '<div id="status" role="status" aria-live="polite"><div class="spinner" aria-label="Loading"></div></div>'
REPLACEMENTS = [
    ("html, body { background: #ffffff; color: #1c1c1e; }", f"html, body {{ background: {light}; color: #1c1c1e; }}"),
    ("html, body { background: #000000; color: #f2f2f7; }", f"html, body {{ background: {dark}; color: #f2f2f7; }}"),
    ('<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)">',
     f'<meta name="theme-color" content="{light}" media="(prefers-color-scheme: light)">'),
    ('<meta name="theme-color" content="#000000" media="(prefers-color-scheme: dark)">',
     f'<meta name="theme-color" content="{dark}" media="(prefers-color-scheme: dark)">'),
    ("  @keyframes appear { from { opacity: 0; } }",
     "  @keyframes appear { from { opacity: 0; } }\n"
     "  .splash-pet { width: 112px; height: 112px; object-fit: contain; animation: appear .2s .4s both; }"),
    (STATUS,
     f'<div id="status" role="status" aria-live="polite"><img class="splash-pet" src="{still}" alt="" width="112" height="112">'
     '<div class="spinner" aria-label="Loading"></div></div>'),
]
for old, new in REPLACEMENTS:
    if html.count(old) != 1:
        sys.exit(f"web-splash: index.html changed, not found once: {old[:70]}")
    html = html.replace(old, new)
open(page, "w", encoding="utf-8").write(html)
print(f"web-splash: {pet} on {light} / {dark}")
