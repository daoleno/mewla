#!/usr/bin/env bash
# The Linux compatibility sweep: builds mewla with the terminal engine and runs
# daemon/compatsweep, every scenario on its own fresh HOME, state directory,
# port and tmux directory. Results land in <out>/results.jsonl, one line per
# scenario (pass or fail, with what was seen).
#
#   scripts/tests/linux_sweep.sh [out-dir]                 fake agents only
#   MEWLA_SWEEP_REAL=claude,codex scripts/tests/linux_sweep.sh [out-dir]
#                                                          plus real agents
#   MEWLA_SWEEP_MOUNTS=1 ...                               plus TMPDIR on a
#                                                          noexec tmpfs and a
#                                                          read-only folder (a
#                                                          user namespace)
#   MEWLA_SWEEP_UNIT=1 ...                                 plus mewla update and
#                                                          restart on a systemd
#                                                          user unit (docker)
#
# Real-agent scenarios copy this computer's claude/codex sign-ins into the
# scenario's HOME (never changing ~/.claude or ~/.codex) and spend model calls.
# Needs go, tmux, sqlite3 and the libghostty-vt cache that
# scripts/build-libghostty-desktop.sh writes.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${1:-$(mktemp -d "${TMPDIR:-/tmp}/mewla-sweep-out.XXXXXX")}"
mkdir -p "$OUT"
BUILD="$(mktemp -d "${TMPDIR:-/tmp}/mewla-sweep-bin.XXXXXX")"
trap 'rm -rf "$BUILD"' EXIT
CACHE="${MEWLA_GHOSTTY_DESKTOP_CACHE:-$HOME/.cache/mewla/ghostty-desktop}"
(cd "$ROOT/daemon" && CGO_ENABLED=1 CGO_CFLAGS="-I$CACHE/include" CGO_LDFLAGS="-L$CACHE/linux_amd64" \
  go build -tags ghosttyvt -buildvcs=false -o "$BUILD/mewla" ./cmd/mewla)

status=0
(cd "$ROOT/daemon" && MEWLA_SWEEP_BINARY="$BUILD/mewla" MEWLA_SWEEP_OUT="$OUT" \
  go test ./compatsweep -count=1 -v -timeout 90m) || status=$?
if [[ "${MEWLA_SWEEP_MOUNTS:-}" == 1 ]]; then
  # Mounts need no root inside a user namespace of their own; they vanish
  # with it.
  MOUNTS="$BUILD/mounts"
  mkdir -p "$MOUNTS/noexec" "$MOUNTS/ro" "$BUILD/ro-src/project"
  (cd "$ROOT/daemon" && MEWLA_SWEEP_BINARY="$BUILD/mewla" MEWLA_SWEEP_OUT="$OUT" unshare -Urm sh -c '
    set -e
    mount -t tmpfs -o noexec,nosuid tmpfs "$1/noexec"
    mount --bind "$2" "$1/ro" && mount -o remount,bind,ro "$1/ro"
    MEWLA_SWEEP_MOUNTS="$1" exec go test ./compatsweep -run TestMounts -count=1 -v -timeout 10m
  ' sh "$MOUNTS" "$BUILD/ro-src") || status=$?
fi
if [[ "${MEWLA_SWEEP_UNIT:-}" == 1 ]]; then
  "$ROOT/scripts/tests/linux_unit_update.sh" "$OUT" || status=$?
fi
echo
python3 -I - "$OUT/results.jsonl" <<'PY' || true
import json, sys
for line in open(sys.argv[1]):
    r = json.loads(line)
    print("%-5s %-40s %s" % (r["outcome"].upper(), r["scenario"], r["detail"].splitlines()[0][:110] if r["detail"] else ""))
PY
echo "results: $OUT/results.jsonl"
exit "$status"
