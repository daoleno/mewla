#!/usr/bin/env bash
# Build the static libghostty-vt the daemon's session host links
# (daemon/ghosttyvt, build tag ghosttyvt).
#
# Same rules as build-libghostty.sh: the signed vt-X.Y.Z release tarballs
# pinned in native.lock.json, unmodified, built offline with the pinned Zig.
#
# Usage:
#   ./scripts/build-libghostty-desktop.sh                      # this machine's target
#   ./scripts/build-libghostty-desktop.sh --targets linux_amd64,darwin_arm64
#   ./scripts/build-libghostty-desktop.sh --out DIR            # default: daemon/ghostty-libs
#
# DIR gets include/ghostty/, one <goos>_<goarch>/libghostty-vt.a per target and
# manifest.json. An up-to-date DIR is left alone; otherwise the new one replaces
# it whole, so a reader never sees a half-written directory. Targets already in
# DIR from the same derivation are kept.
#
# daemon/ghostty-libs is gitignored, and mewla-dev builds the daemon with the
# ghosttyvt tag whenever it exists: only create it on purpose. Tests and CI use
# --out with a cache outside the checkout (scripts/test-ghosttyvt.sh).
#
# Requires python3 and curl. Release tarballs are cached in
# ${MEWLA_GHOSTTY_CACHE:-$HOME/.cache/mewla/ghostty}; the pinned Zig in
# ${MEWLA_TOOL_CACHE:-.cache/mewla-tools}.

set -euo pipefail
umask 022

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/native/terminal-vt/native.lock.json"

"$ROOT/scripts/verify-libghostty.sh" --contract >/dev/null

OUT=""
TARGETS=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="${2:?--out needs a directory}"; shift 2 ;;
    --targets) TARGETS="${2:?--targets needs a list}"; shift 2 ;;
    -h|--help) sed -n '2,24p' "$0"; exit 0 ;;
    *) echo "error: unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$OUT" ]]; then
  OUT="$ROOT/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["desktop"]["lib_dir"])' "$LOCK")"
fi
if [[ "$OUT" != /* ]]; then
  OUT="$PWD/$OUT"
fi
if [[ -z "$TARGETS" ]]; then
  case "$(uname -s)/$(uname -m)" in
    Linux/x86_64|Linux/amd64) TARGETS=linux_amd64 ;;
    Linux/aarch64|Linux/arm64) TARGETS=linux_arm64 ;;
    Darwin/arm64) TARGETS=darwin_arm64 ;;
    *) echo "error: no desktop libghostty-vt target for $(uname -s)/$(uname -m)" >&2; exit 2 ;;
  esac
fi

BUILD_TMP_ROOT="${MEWLA_BUILD_TMPDIR:-${XDG_CACHE_HOME:-$HOME/.cache}/mewla/build-tmp}"
if [[ "$BUILD_TMP_ROOT" != /* ]]; then
  echo "error: MEWLA_BUILD_TMPDIR must be an absolute path: $BUILD_TMP_ROOT" >&2
  exit 1
fi
mkdir -p "$BUILD_TMP_ROOT"
BUILD_WORK="$(mktemp -d "$BUILD_TMP_ROOT/libghostty-desktop.XXXXXX")"
STAGE=""
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  rm -rf -- "$BUILD_WORK"
  if [[ -n "$STAGE" && -d "$STAGE" ]]; then
    rm -rf -- "$STAGE"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# The derivation records the lock's Zig; mewla_resolve_zig only ever runs a
# Zig of that version, and only when something has to be built.
ZIG_VER="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["zig"]["version"])' "$LOCK")"

# plan.py prints one "<go> <zig_target> <reuse|build>" line per target to
# produce, or nothing when OUT is already up to date.
PLAN="$(python3 - "$LOCK" "$OUT" "$TARGETS" "$ZIG_VER" <<'PY'
import hashlib, json, sys
from pathlib import Path

lock = json.load(open(sys.argv[1]))
out, wanted, zig = Path(sys.argv[2]), sys.argv[3].split(","), sys.argv[4]
desktop = lock["desktop"]
release = lock["ghostty"]["release"]
known = {t["go"]: t["zig_target"] for t in desktop["targets"]}
for name in wanted:
    if name not in known:
        sys.exit(f"error: target {name!r} is not in native.lock.json desktop.targets ({', '.join(known)})")

derivation = {
    "ghostty_version": lock["ghostty"]["version"],
    "source_sha256": release["source"]["sha256"],
    "deps_sha256": release["deps"]["sha256"],
    "headers_sha256": lock["ghostty"]["headers_sha256"],
    "zig_version": zig,
}
reusable = {}
try:
    manifest = json.load(open(out / desktop["manifest_filename"]))
    if all(manifest.get(k) == v for k, v in derivation.items()):
        for entry in manifest.get("targets", []):
            lib = out / entry["go"] / desktop["library_filename"]
            if entry.get("zig_target") == known.get(entry["go"]) and lib.is_file() \
                    and hashlib.sha256(lib.read_bytes()).hexdigest() == entry.get("sha256"):
                reusable[entry["go"]] = entry["zig_target"]
except (OSError, ValueError, KeyError):
    pass

keep = sorted(set(reusable) | set(wanted))
if all(name in reusable for name in wanted) and set(keep) == set(reusable):
    sys.exit(0)
for name in keep:
    print(name, known[name], "reuse" if name in reusable else "build")
PY
)"
if [[ -z "$PLAN" ]]; then
  echo "libghostty-vt desktop libraries in $OUT are up to date ($TARGETS)"
  exit 0
fi

# shellcheck source=lib/zig.sh
source "$ROOT/scripts/lib/zig.sh"
ZIG_BIN="$(mewla_resolve_zig "$LOCK" "${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}")"
echo "zig: $("$ZIG_BIN" version) ($ZIG_BIN)"

RELEASE_JSON="$(python3 "$ROOT/scripts/lib/ghostty_release.py" "$LOCK" prepare \
  --cache "${MEWLA_GHOSTTY_CACHE:-$HOME/.cache/mewla/ghostty}/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["ghostty"]["tag"])' "$LOCK")" \
  --dest "$BUILD_WORK/release" \
  ${MEWLA_GHOSTTY_RELEASE_DIR:+--release-dir "$MEWLA_GHOSTTY_RELEASE_DIR"})"
release_field() {
  python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$RELEASE_JSON" "$1"
}
SOURCE_DIR="$(release_field source_dir)"
DEPS_DIR="$(release_field deps_dir)"

NEW="$BUILD_WORK/out"
mkdir -p "$NEW/include"
cp -R "$SOURCE_DIR/include/ghostty" "$NEW/include/ghostty"

while read -r go_target zig_target action; do
  mkdir -p "$NEW/$go_target"
  if [[ "$action" == "reuse" ]]; then
    cp "$OUT/$go_target/libghostty-vt.a" "$NEW/$go_target/libghostty-vt.a"
    echo "=== $go_target ($zig_target): kept"
    continue
  fi
  echo "=== $go_target ($zig_target): building"
  prefix="$BUILD_WORK/prefix-$go_target"
  # A fresh, empty global cache: with --system it proves the build needs
  # nothing but the release tarballs.
  zig_cache="$(mktemp -d "$BUILD_WORK/zigcache.XXXXXX")"
  (
    cd "$SOURCE_DIR"
    ZIG_GLOBAL_CACHE_DIR="$zig_cache" "$ZIG_BIN" build \
      --system "$DEPS_DIR" \
      --prefix "$prefix" \
      -Doptimize=ReleaseFast "-Dtarget=$zig_target"
  )
  rm -rf -- "$zig_cache"
  # Zig names the Windows static library ghostty-vt-static.lib; it is the same
  # kind of archive, kept under the one name the cgo directives link.
  lib="$prefix/lib/libghostty-vt.a"
  [[ "$zig_target" != *-windows-* ]] || lib="$prefix/lib/ghostty-vt-static.lib"
  cp "$lib" "$NEW/$go_target/libghostty-vt.a"
done <<<"$PLAN"

python3 - "$LOCK" "$NEW" "$ZIG_VER" "$(release_field signature_verified)" "$PLAN" <<'PY'
import hashlib, json, sys
from pathlib import Path

lock = json.load(open(sys.argv[1]))
new, zig, signed, plan = Path(sys.argv[2]), sys.argv[3], sys.argv[4] == "True", sys.argv[5]
desktop = lock["desktop"]
release = lock["ghostty"]["release"]
targets = []
for line in plan.splitlines():
    go, zig_target, _ = line.split()
    lib = new / go / desktop["library_filename"]
    targets.append({"go": go, "zig_target": zig_target,
                    "sha256": hashlib.sha256(lib.read_bytes()).hexdigest(), "size": lib.stat().st_size})
manifest = {
    "schema_version": 1,
    "ghostty_version": lock["ghostty"]["version"],
    "source_sha256": release["source"]["sha256"],
    "deps_sha256": release["deps"]["sha256"],
    "headers_sha256": lock["ghostty"]["headers_sha256"],
    "zig_version": zig,
    "signature_verified": signed,
    "targets": targets,
}
(new / desktop["manifest_filename"]).write_text(json.dumps(manifest, indent=2) + "\n")
PY

# Swap the whole directory in. Both renames stay inside OUT's parent, so each
# is atomic, and at no point does OUT hold a mix of two builds.
PARENT="$(dirname "$OUT")"
mkdir -p "$PARENT"
STAGE="$(mktemp -d "$PARENT/.$(basename "$OUT").new.XXXXXX")"
OLD=""
cp -R "$NEW/." "$STAGE/"
if [[ -e "$OUT" ]]; then
  OLD="$(mktemp -d "$PARENT/.$(basename "$OUT").old.XXXXXX")"
  mv -- "$OUT" "$OLD/previous"
fi
mv -- "$STAGE" "$OUT"
if [[ -n "$OLD" ]]; then
  rm -rf -- "$OLD"
fi
echo "libghostty-vt desktop libraries written to $OUT"
