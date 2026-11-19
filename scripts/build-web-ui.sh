#!/usr/bin/env bash
# Package the Mewla client for the browser and stage it for embedding in the
# mewla daemon.
#
# `nsh package --target web` writes content-hashed files; each is stored
# gzip-only in daemon/webui/dist and the daemon serves the compressed bytes
# directly. Run before `go build ./cmd/mewla`. Uses the pinned NSH in $NSH_HOME
# (default ~/.nsh; client/scripts/fetch-nsh.sh installs it when missing), never
# another nsh on PATH: the package carries NSH's web runtime, which must be the
# version the app is built and tested with.
#
# Usage:
#   ./scripts/build-web-ui.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/daemon/webui/dist"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/web-ui.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT

"$ROOT/client/scripts/fetch-nsh.sh" >/dev/null
NSH="${NSH_HOME:-$HOME/.nsh}/bin/nsh"

"$NSH" package --target web -o "$STAGE/package" "$ROOT/client/ui"
# The package is one folder named after the app.
APP="$(dirname "$(find "$STAGE/package" -mindepth 2 -maxdepth 2 -name index.html | head -1)")"
test -f "$APP/index.html"
# Every pet image the app names is in the package, where index.html's asset map sends it.
python3 "$ROOT/client/scripts/check-pet-assets.py" web "$APP"
# While the runtime downloads (minutes through a slow tunnel), the page is Mewla's: the app's background and its pet.
python3 -I "$ROOT/scripts/web-splash.py" "$APP" "$ROOT"
# Every file the page names is at the site's root (nsh.json web.base "/"), where the daemon serves
# them, so a deep link such as /sessions/<id> loads them when it is reloaded or opened.
python3 -I "$ROOT/scripts/web-page-root.py" "$APP/index.html"
# The web terminal's page (an NSH web view) loads these from <origin>/terminal/: its script, its VT
# (libghostty-vt with the phones' packer and glyphs, WebAssembly; built once, then cached) and the
# phones' face (client/native/terminal/fonts, SIL OFL 1.1).
mkdir -p "$APP/terminal"
cp "$ROOT/client/ui/web/terminal/terminal.js" "$APP/terminal/terminal.js"
# An assignment, so a failed build stops here (pipefail); its last line is the module's path.
VT_WASM="$("$ROOT/scripts/build-libghostty-web.sh" | tail -1)"
cp "$VT_WASM" "$APP/terminal/mewla-vt.wasm"
for file in MapleMono-NF-Regular.ttf MapleMono-NF-SemiBold.ttf MapleMono-NF-Italic.ttf MapleMono-NF-SemiBoldItalic.ttf; do
  cp "$ROOT/client/native/terminal/fonts/$file" "$APP/terminal/$file"
done
# NSH writes .gz and .br beside some files: keep one gzip of every file.
find "$APP" -type f \( -name '*.gz' -o -name '*.br' \) -delete
find "$APP" -type f -exec gzip -9 -n {} +

# A dev daemon serves $OUT from disk while this runs (mewla-dev), so the new
# package replaces the old one in one rename, never file by file.
NEXT="$OUT.next.$$"
OLD="$OUT.old.$$"
rm -rf "$NEXT"
mkdir -p "$NEXT"
cp -R "$APP/." "$NEXT/"
if [ -f "$OUT/.gitkeep" ]; then cp "$OUT/.gitkeep" "$NEXT/.gitkeep"; fi
if [ -d "$OUT" ]; then mv "$OUT" "$OLD"; fi
mv "$NEXT" "$OUT"
rm -rf "$OLD"
echo "Web UI staged in $OUT ($(du -sh "$OUT" | cut -f1) gzip)"
