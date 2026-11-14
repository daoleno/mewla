#!/usr/bin/env bash
# Build the browser terminal's VT: libghostty-vt for WebAssembly with the phones' packer and glyphs
# (client/native/terminal/TerminalVt.cpp, TerminalGlyphs.cpp) linked in, one mewla-vt.wasm that the
# terminal page (client/ui/web/terminal/terminal.js) loads from <origin>/terminal/.
#
# Same rules as build-libghostty.sh: the signed vt-X.Y.Z release tarballs pinned in native.lock.json,
# unmodified, built offline with the pinned Zig (native.lock.json "web" names the targets and flags).
#
# Usage:
#   ./scripts/build-libghostty-web.sh              # into ${XDG_CACHE_HOME:-~/.cache}/mewla/ghostty-web
#   ./scripts/build-libghostty-web.sh --out DIR
#
# DIR gets mewla-vt.wasm and manifest.json. A DIR built from the same release, Zig, flags and packer
# sources is left alone (prints its path); otherwise the new one replaces it whole.
#
# Requires python3 and curl. Release tarballs are cached in ${MEWLA_GHOSTTY_CACHE:-$HOME/.cache/mewla/ghostty};
# the pinned Zig in ${MEWLA_TOOL_CACHE:-.cache/mewla-tools}.

set -euo pipefail
umask 022

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/native/terminal-vt/native.lock.json"
NATIVE="$ROOT/client/native/terminal"

"$ROOT/scripts/verify-libghostty.sh" --contract >/dev/null

OUT="${XDG_CACHE_HOME:-$HOME/.cache}/mewla/ghostty-web"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="${2:?--out needs a directory}"; shift 2 ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "error: unknown arg: $1" >&2; exit 2 ;;
  esac
done
[[ "$OUT" == /* ]] || OUT="$PWD/$OUT"

# What the output is made from: if the manifest in OUT records the same, it is current.
DERIVATION="$(python3 - "$LOCK" "$NATIVE" <<'PY'
import hashlib, json, sys
from pathlib import Path

lock = json.load(open(sys.argv[1]))
native = Path(sys.argv[2])
web = lock["web"]
release = lock["ghostty"]["release"]
sources = {name: hashlib.sha256((native / name).read_bytes()).hexdigest() for name in web["packer_sources"]}
print(json.dumps({
    "ghostty_version": lock["ghostty"]["version"],
    "source_sha256": release["source"]["sha256"],
    "deps_sha256": release["deps"]["sha256"],
    "headers_sha256": lock["ghostty"]["headers_sha256"],
    "zig_version": lock["zig"]["version"],
    "zig_target": web["zig_target"],
    "optimize": web["optimize"],
    "packer_target": web["packer_target"],
    "packer_flags": web["packer_flags"],
    "exports": web["exports"],
    "packer_sources": sources,
}, sort_keys=True))
PY
)"
if python3 - "$OUT" "$DERIVATION" <<'PY'
import hashlib, json, sys
from pathlib import Path
out, want = Path(sys.argv[1]), json.loads(sys.argv[2])
try:
    manifest = json.load(open(out / "manifest.json"))
    wasm = (out / "mewla-vt.wasm").read_bytes()
except (OSError, ValueError):
    sys.exit(1)
ok = manifest.get("derivation") == want and hashlib.sha256(wasm).hexdigest() == manifest.get("sha256")
sys.exit(0 if ok else 1)
PY
then
  echo "$OUT/mewla-vt.wasm"
  exit 0
fi

BUILD_TMP_ROOT="${MEWLA_BUILD_TMPDIR:-${XDG_CACHE_HOME:-$HOME/.cache}/mewla/build-tmp}"
[[ "$BUILD_TMP_ROOT" == /* ]] || { echo "error: MEWLA_BUILD_TMPDIR must be an absolute path: $BUILD_TMP_ROOT" >&2; exit 1; }
mkdir -p "$BUILD_TMP_ROOT"
BUILD_WORK="$(mktemp -d "$BUILD_TMP_ROOT/libghostty-web.XXXXXX")"
STAGE=""
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  rm -rf -- "$BUILD_WORK"
  [[ -z "$STAGE" || ! -d "$STAGE" ]] || rm -rf -- "$STAGE"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

field() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))["web"][sys.argv[2]]; print(" ".join(v) if isinstance(v, list) else v)' "$LOCK" "$1"; }

# shellcheck source=lib/zig.sh
source "$ROOT/scripts/lib/zig.sh"
ZIG_BIN="$(mewla_resolve_zig "$LOCK" "${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}")"
echo "zig: $("$ZIG_BIN" version) ($ZIG_BIN)" >&2

RELEASE_JSON="$(python3 "$ROOT/scripts/lib/ghostty_release.py" "$LOCK" prepare \
  --cache "${MEWLA_GHOSTTY_CACHE:-$HOME/.cache/mewla/ghostty}/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["ghostty"]["tag"])' "$LOCK")" \
  --dest "$BUILD_WORK/release" \
  ${MEWLA_GHOSTTY_RELEASE_DIR:+--release-dir "$MEWLA_GHOSTTY_RELEASE_DIR"})"
release_field() { python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$RELEASE_JSON" "$1"; }
SOURCE_DIR="$(release_field source_dir)"
DEPS_DIR="$(release_field deps_dir)"

# libghostty-vt for the browser (a static library; the release's own ghostty-vt.wasm is not used).
# A fresh, empty Zig cache: with --system it proves the build needs nothing but the release tarballs.
ZIG_CACHE="$BUILD_WORK/zig-cache"
mkdir -p "$ZIG_CACHE"
(
  cd "$SOURCE_DIR"
  ZIG_GLOBAL_CACHE_DIR="$ZIG_CACHE" ZIG_LOCAL_CACHE_DIR="$ZIG_CACHE" "$ZIG_BIN" build \
    --system "$DEPS_DIR" \
    --prefix "$BUILD_WORK/prefix" \
    -Demit-lib-vt "-Doptimize=$(field optimize)" "-Dtarget=$(field zig_target)" >&2
)

# The packer and glyphs, linked with it into one module that exports only what the page calls.
exports=()
for name in $(field exports); do exports+=("-Wl,--export=$name"); done
sources=()
for name in $(field packer_sources); do [[ "$name" == *.cpp ]] && sources+=("$NATIVE/$name"); done
# shellcheck disable=SC2046
ZIG_GLOBAL_CACHE_DIR="$ZIG_CACHE" ZIG_LOCAL_CACHE_DIR="$ZIG_CACHE" "$ZIG_BIN" c++ \
  -target "$(field packer_target)" $(field packer_flags) \
  -I"$SOURCE_DIR/include" -I"$NATIVE" \
  "${sources[@]}" "$BUILD_WORK/prefix/lib/libghostty-vt.a" \
  "${exports[@]}" -o "$BUILD_WORK/mewla-vt.wasm"

STAGE="$(mktemp -d "$(dirname "$OUT")/.ghostty-web.XXXXXX" 2>/dev/null || { mkdir -p "$(dirname "$OUT")"; mktemp -d "$(dirname "$OUT")/.ghostty-web.XXXXXX"; })"
cp "$BUILD_WORK/mewla-vt.wasm" "$STAGE/mewla-vt.wasm"
python3 - "$STAGE" "$DERIVATION" "$(release_field signature_verified)" <<'PY'
import hashlib, json, sys
from pathlib import Path
stage = Path(sys.argv[1])
wasm = (stage / "mewla-vt.wasm").read_bytes()
json.dump({"schema_version": 1, "derivation": json.loads(sys.argv[2]), "signature_verified": sys.argv[3] == "True",
           "sha256": hashlib.sha256(wasm).hexdigest(), "size": len(wasm)},
          open(stage / "manifest.json", "w"), indent=2, sort_keys=True)
PY
chmod 755 "$STAGE"
rm -rf -- "$OUT"
mv "$STAGE" "$OUT"
STAGE=""
echo "$OUT/mewla-vt.wasm"
