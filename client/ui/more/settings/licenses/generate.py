#!/usr/bin/env python3
"""Writes licenses.json: what the NSH client ships and under which license.

The list is kept by hand (the client has no package manager to read): the NSH
runtime, the code bundled into the app, and the native libraries of
client/android/app/build.gradle.kts and client/ios/project.yml. License texts
come from the files beside this script, so a change is a reviewed diff.

    python3 client/ui/more/settings/licenses/generate.py           # rewrite
    python3 client/ui/more/settings/licenses/generate.py --check   # fail if stale
"""
import hashlib, json, pathlib, sys

here = pathlib.Path(__file__).parent
texts = {}

def text(name):
    body = (here / "texts" / name).read_text()
    key = hashlib.sha256(body.encode()).hexdigest()[:12]
    texts[key] = body
    return key

APACHE = text("Apache-2.0.txt")
sections = [
    {"title": "Bundled code", "items": [
        {"name": "Native Shell (NSH)", "license": "Apache-2.0", "url": "https://nsh.dev", "text": APACHE,
         "note": "The native UI framework the app is built on."},
        {"name": "Ghostty (libghostty-vt)", "license": "MIT", "url": "https://github.com/ghostty-org/ghostty", "text": text("Ghostty-MIT.txt")},
        {"name": "noVNC", "version": "1.7.0", "license": "MPL-2.0", "url": "https://github.com/novnc/noVNC", "text": text("noVNC-MPL-2.0.txt"),
         "note": "Draws a computer's screen in Screen. Its source is the published 1.7.0 release, unmodified apart from how it is bundled."},
        {"name": "TweetNaCl.js", "version": "1.0.3", "license": "Unlicense", "url": "https://github.com/dchest/tweetnacl-js", "text": text("Unlicense.txt")},
    ]},
    {"title": "Native libraries", "items": [
        {"name": "AndroidX (Activity, Core, CameraX, Lifecycle)", "license": "Apache-2.0", "url": "https://developer.android.com/jetpack/androidx", "platforms": ["android"], "text": APACHE},
        {"name": "Firebase Cloud Messaging", "license": "Apache-2.0", "url": "https://github.com/firebase/firebase-android-sdk", "platforms": ["android"], "text": APACHE},
        {"name": "ML Kit Barcode Scanning", "license": "Google terms", "url": "https://developers.google.com/ml-kit/terms", "platforms": ["android"],
         "note": "Reads the pairing code from the camera. Used under the ML Kit Terms of Service: https://developers.google.com/ml-kit/terms"},
    ]},
]
output = json.dumps({"sections": sections, "texts": texts}, indent=1, ensure_ascii=False) + "\n"
path = here / "licenses.json"
if "--check" in sys.argv:
    if not path.exists() or path.read_text() != output:
        sys.exit("stale: client/ui/more/settings/licenses/licenses.json (run generate.py)")
else:
    path.write_text(output)
    print(f"wrote {path.name}: {sum(len(s['items']) for s in sections)} items, {len(texts)} texts")
