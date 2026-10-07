#!/usr/bin/env bash
# Build the local Mewla daemon without native display dependencies.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/bin/mewla}"
if [[ "$OUT" != /* ]]; then
  OUT="$ROOT/$OUT"
fi
mkdir -p "$(dirname "$OUT")"
PUBLISHER_FLAGS="$(python3 "$ROOT/scripts/plugin-publisher-flags.py")"
cd "$ROOT/daemon"
CGO_ENABLED=0 go build -ldflags="$PUBLISHER_FLAGS" -o "$OUT" ./cmd/mewla
chmod +x "$OUT"
