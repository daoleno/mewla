#!/usr/bin/env bash
# Package the Mewla client for the browser and stage it for embedding in the
# mewla daemon.
#
# `nsh package --target web` writes content-hashed files; each is stored
# gzip-only in daemon/webui/dist and the daemon serves the compressed bytes
# directly. Run before `go build ./cmd/mewla`. Uses the NSH on PATH, else the
# pinned one in ~/.nsh (client/scripts/fetch-nsh.sh installs it).
#
# Usage:
#   ./scripts/build-web-ui.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/daemon/webui/dist"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/web-ui.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT

NSH="$(command -v nsh || true)"
if [[ -z "$NSH" ]]; then
  "$ROOT/client/scripts/fetch-nsh.sh" >/dev/null
  NSH="${NSH_HOME:-$HOME/.nsh}/bin/nsh"
fi

"$NSH" package --target web -o "$STAGE/package" "$ROOT/client/ui"
# The package is one folder named after the app.
APP="$(dirname "$(find "$STAGE/package" -mindepth 2 -maxdepth 2 -name index.html | head -1)")"
test -f "$APP/index.html"
# NSH writes .gz and .br beside some files: keep one gzip of every file.
find "$APP" -type f \( -name '*.gz' -o -name '*.br' \) -delete
find "$APP" -type f -exec gzip -9 -n {} +

find "$OUT" -mindepth 1 ! -name .gitkeep -delete
cp -R "$APP/." "$OUT/"
echo "Web UI staged in $OUT ($(du -sh "$OUT" | cut -f1) gzip)"
