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
        # Compiled into libghostty-vt (deps/libghostty-vt-third-party.json of the pinned release tarball).
        {"name": "Highway", "version": "1.2.0", "license": "Apache-2.0 OR BSD-3-Clause", "url": "https://github.com/google/highway", "platforms": ["android", "ios"],
         "text": text("libghostty-highway.txt"), "note": "Compiled into libghostty-vt, the terminal core of the app."},
        {"name": "simdutf", "version": "9.0.0", "license": "Apache-2.0 OR MIT", "url": "https://github.com/simdutf/simdutf", "platforms": ["android", "ios"],
         "text": text("libghostty-simdutf.txt"), "note": "Compiled into libghostty-vt, the terminal core of the app."},
        {"name": "uucode", "version": "0.2.0", "license": "MIT AND Unicode-3.0", "url": "https://github.com/jacobsandlund/uucode", "platforms": ["android", "ios"],
         "text": text("libghostty-uucode.txt"), "note": "Compiled into libghostty-vt, the terminal core of the app."},
        {"name": "Wuffs", "version": "0.4.0-alpha.10", "license": "Apache-2.0 OR MIT", "url": "https://github.com/google/wuffs", "platforms": ["android", "ios"],
         "text": text("libghostty-wuffs.txt"), "note": "Compiled into libghostty-vt, the terminal core of the app."},
        {"name": "translate-c", "version": "2.0.0", "license": "MIT", "url": "https://ziglang.org", "platforms": ["android", "ios"],
         "text": text("libghostty-translate-c.txt"), "note": "Compiled into libghostty-vt, the terminal core of the app."},
        {"name": "Maple Mono NF", "version": "7.9", "license": "OFL-1.1", "url": "https://github.com/subframe7536/maple-font",
         "text": text("MapleMono-OFL.txt"), "note": "The terminal's font, with the Nerd Fonts icons Maple patches in; subset by scripts/subset-terminal-font.py."},
        {"name": "noVNC", "version": "1.7.0", "license": "MPL-2.0", "url": "https://github.com/novnc/noVNC", "text": text("noVNC-MPL-2.0.txt"),
         "note": "Draws a computer's screen in Screen. Its source is the published 1.7.0 release, unmodified apart from how it is bundled."},
        {"name": "xterm.js", "version": "6.0.0", "license": "MIT", "url": "https://github.com/xtermjs/xterm.js", "platforms": ["web"], "text": text("xterm-MIT.txt"),
         "note": "Draws a Session's terminal in the browser; served by the computer beside the web app."},
        {"name": "TweetNaCl.js", "version": "1.0.3", "license": "Unlicense", "url": "https://github.com/dchest/tweetnacl-js", "text": text("Unlicense.txt")},
        # The agent marks in ui/icons/agents (AgentIcon.tsx says where each comes from).
        {"name": "Lobe Icons", "version": "2.21.0", "license": "MIT", "url": "https://github.com/lobehub/lobe-icons", "text": text("lobe-icons-MIT.txt"),
         "note": "The Claude, Codex, Grok, OpenCode and DeepSeek marks."},
        {"name": "Pi Coding Agent mark", "license": "MIT", "url": "https://pi.dev", "text": text("Pi-mark-MIT.txt")},
    ]},
    {"title": "Native libraries", "items": [
        {"name": "AndroidX (Activity, Core, CameraX, Lifecycle)", "license": "Apache-2.0", "url": "https://developer.android.com/jetpack/androidx", "platforms": ["android"], "text": APACHE},
        {"name": "OkHttp", "version": "4.12.0", "license": "Apache-2.0", "url": "https://github.com/square/okhttp", "platforms": ["android"], "text": APACHE,
         "note": "Phone use's link to the computer (GitHub APK)."},
        {"name": "kotlinx.coroutines", "version": "1.10.2", "license": "Apache-2.0", "url": "https://github.com/Kotlin/kotlinx.coroutines", "platforms": ["android"], "text": APACHE},
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
