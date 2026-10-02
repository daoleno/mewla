#!/usr/bin/env bash
# Build the local Zen daemon without native display dependencies.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/bin/zen}"
if [[ "$OUT" != /* ]]; then
  OUT="$ROOT/$OUT"
fi
mkdir -p "$(dirname "$OUT")"
cd "$ROOT/daemon"
CGO_ENABLED=0 go build -o "$OUT" ./cmd/zen
chmod +x "$OUT"
