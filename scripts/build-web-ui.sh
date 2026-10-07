#!/usr/bin/env bash
# Export the Expo web app and stage it for embedding in the mewla daemon.
#
# Every exported file is stored gzip-only in daemon/webui/dist; the daemon
# serves the compressed bytes directly. Run before `go build ./cmd/mewla`.
# Requires `bun install` at the repository root.
#
# Usage:
#   ./scripts/build-web-ui.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/daemon/webui/dist"
STAGE="$(mktemp -d "${TMPDIR:-/tmp}/zen-web-ui.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT

(
  cd "$ROOT/app"
  bunx expo export --platform web --output-dir "$STAGE/export"
)
test -f "$STAGE/export/index.html"
find "$STAGE/export" -type f -exec gzip -9 -n {} +

find "$OUT" -mindepth 1 ! -name .gitkeep -delete
cp -R "$STAGE/export/." "$OUT/"
echo "Web UI staged in $OUT ($(du -sh "$OUT" | cut -f1) gzip)"
