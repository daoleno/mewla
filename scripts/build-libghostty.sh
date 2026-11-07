#!/usr/bin/env bash
# Build libghostty-vt for the Android ABIs declared in native.lock.json.
#
# The source is the signed vt-X.Y.Z release tarball and its companion
# dependency tarball (see PACKAGING-LIBGHOSTTY.md inside the tarball), taken
# unmodified: sha256 pinned in the lock, minisign checked when a verifier is
# available, built offline with `zig build --system`. Never a git checkout.
#
# Usage:
#   ./scripts/build-libghostty.sh
#   ./scripts/build-libghostty.sh --abis arm64-v8a
#   MEWLA_GHOSTTY_RELEASE_DIR=/dir/with/tarballs ./scripts/build-libghostty.sh  # offline
#   MEWLA_BUILD_TMPDIR=/durable/path ./scripts/build-libghostty.sh
#
# Requires: python3 and curl. The pinned Zig from native.lock.json is used from
# PATH when it matches, otherwise fetched into ${MEWLA_TOOL_CACHE:-.cache/mewla-tools}.
# Release tarballs are cached in ${MEWLA_GHOSTTY_CACHE:-$HOME/.cache/mewla/ghostty}.

set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/terminal-vt/native.lock.json"
NOTICE_SRC="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"

if [[ ! -f "$LOCK" ]]; then
  echo "error: missing lockfile $LOCK" >&2
  exit 1
fi

if [[ ! -f "$NOTICE_SRC" ]]; then
  echo "error: missing Ghostty MIT notice $NOTICE_SRC" >&2
  exit 1
fi

# Validate the complete lock and its packaging consumers before deriving any
# native output.
"$ROOT/scripts/verify-libghostty.sh" --contract

default_build_tmpdir() {
  if [[ -n "${XDG_CACHE_HOME:-}" ]]; then
    printf '%s\n' "$XDG_CACHE_HOME/mewla/build-tmp"
  elif [[ "$(uname -s)" == "Darwin" ]]; then
    printf '%s\n' "$HOME/Library/Caches/mewla/build-tmp"
  else
    printf '%s\n' "$HOME/.cache/mewla/build-tmp"
  fi
}

# Large native builds must not fall back to the host's global temporary
# filesystem. Delegated sessions set MEWLA_BUILD_TMPDIR to their private
# Mewla-owned resource directory, so lifecycle teardown reclaims it. Direct
# developer builds use a durable platform cache and retain the normal
# EXIT/INT/TERM cleanup below.
BUILD_TMP_ROOT="${MEWLA_BUILD_TMPDIR:-$(default_build_tmpdir)}"
if [[ "$BUILD_TMP_ROOT" != /* ]]; then
  echo "error: MEWLA_BUILD_TMPDIR must be an absolute path: $BUILD_TMP_ROOT" >&2
  exit 1
fi
mkdir -p "$BUILD_TMP_ROOT"
chmod 700 "$BUILD_TMP_ROOT"

LOCK_ENV="$(mktemp "$BUILD_TMP_ROOT/libghostty-lock.XXXXXX")"
BUILD_WORK=""
ZIG_CACHE_DIR=""

cleanup() {
  local exit_status=$?
  trap - EXIT INT TERM
  if [[ -n "$ZIG_CACHE_DIR" && -d "$ZIG_CACHE_DIR" ]]; then
    rm -rf -- "$ZIG_CACHE_DIR"
  fi
  if [[ -n "$BUILD_WORK" && -d "$BUILD_WORK" ]]; then
    rm -rf -- "$BUILD_WORK"
  fi
  rm -f "$LOCK_ENV"
  exit "$exit_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

python3 - "$LOCK" >"$LOCK_ENV" <<'PY'
import json, sys
lock = json.load(open(sys.argv[1]))
print(f"LOCK_GHOSTTY_VERSION={lock['ghostty']['version']}")
print(f"LOCK_GHOSTTY_TAG={lock['ghostty']['tag']}")
print(f"LOCK_GHOSTTY_COMMIT={lock['ghostty']['commit']}")
print(f"LOCK_HEADERS_SHA256={lock['ghostty']['headers_sha256']}")
print(f"LOCK_ZIG_VERSION={lock['zig']['version']}")
print(f"LOCK_LIB_DIR={lock['outputs']['lib_dir']}")
print(f"LOCK_LIB_NAME={lock['outputs']['library_filename']}")
print(f"LOCK_HEADERS_DIR={lock['outputs']['headers_dir']}")
print(f"LOCK_CHECKSUMS={lock['outputs']['checksums_filename']}")
print(f"LOCK_MANIFEST={lock['outputs']['manifest_filename']}")
print(f"LOCK_MIN_API={lock['android']['min_api']}")
abis = lock["abis"]
print("LOCK_ABI_COUNT=%d" % len(abis))
allowed = []
for i, a in enumerate(abis):
    print(f"LOCK_ABI_{i}_ANDROID={a['android_abi']}")
    print(f"LOCK_ABI_{i}_ZIG={a['zig_target']}")
    print(f"LOCK_ABI_{i}_ELF={a['elf_machine_code']}")
    allowed.append(a["android_abi"])
print("LOCK_ALLOWED_ABIS=" + ",".join(allowed))
PY
# shellcheck disable=SC1090
source "$LOCK_ENV"

ONLY_ABIS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --abis)
      shift
      IFS=',' read -r -a ONLY_ABIS <<<"${1:-}"
      shift || true
      ;;
    -h|--help)
      sed -n '2,16p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

IFS=',' read -r -a ALLOWED_ABIS <<<"$LOCK_ALLOWED_ABIS"
if [[ ${#ONLY_ABIS[@]} -gt 0 ]]; then
  for abi in "${ONLY_ABIS[@]}"; do
    ok=0
    for a in "${ALLOWED_ABIS[@]}"; do
      [[ "$a" == "$abi" ]] && ok=1 && break
    done
    if [[ $ok -ne 1 ]]; then
      echo "error: ABI '$abi' not in lock contract (${LOCK_ALLOWED_ABIS})" >&2
      exit 2
    fi
  done
fi

need_abi() {
  local abi="$1"
  if [[ ${#ONLY_ABIS[@]} -eq 0 ]]; then
    return 0
  fi
  local x
  for x in "${ONLY_ABIS[@]}"; do
    [[ "$x" == "$abi" ]] && return 0
  done
  return 1
}

# shellcheck source=lib/zig.sh
source "$ROOT/scripts/lib/zig.sh"
ZIG_BIN="$(mewla_resolve_zig "$LOCK" "${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}")"
ZIG_VER="$("$ZIG_BIN" version)"

BUILD_WORK="$(mktemp -d "$BUILD_TMP_ROOT/libghostty-build.XXXXXX")"
RELEASE_JSON="$(python3 "$ROOT/scripts/lib/ghostty_release.py" "$LOCK" prepare \
  --cache "${MEWLA_GHOSTTY_CACHE:-$HOME/.cache/mewla/ghostty}/$LOCK_GHOSTTY_TAG" \
  --dest "$BUILD_WORK/release" \
  ${MEWLA_GHOSTTY_RELEASE_DIR:+--release-dir "$MEWLA_GHOSTTY_RELEASE_DIR"})"
release_field() {
  python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$RELEASE_JSON" "$1"
}
SOURCE_DIR="$(release_field source_dir)"
DEPS_DIR="$(release_field deps_dir)"
SOURCE_SHA256="$(release_field source_sha256)"
DEPS_SHA256="$(release_field deps_sha256)"
SIGNATURE_VERIFIED="$(release_field signature_verified)"

OUT_DIR="$ROOT/$LOCK_LIB_DIR"
HEADER_DST="$ROOT/$LOCK_HEADERS_DIR"
mkdir -p "$OUT_DIR"

echo "Building libghostty-vt $LOCK_GHOSTTY_VERSION"
echo "  release: $LOCK_GHOSTTY_TAG (source sha256 ${SOURCE_SHA256:0:12}…, deps ${DEPS_SHA256:0:12}…, minisign verified: $SIGNATURE_VERIFIED)"
echo "  source:  $SOURCE_DIR (disposable)"
echo "  zig:     $ZIG_VER"
echo "  out:     $OUT_DIR"

SUMS_FILE="$OUT_DIR/$LOCK_CHECKSUMS"
MANIFEST="$OUT_DIR/$LOCK_MANIFEST"

# A subset build may retain an unbuilt ABI only when its existing manifest
# proves that it came from this exact release and Zig, and its artifact still
# matches the recorded digest and ABI contract. Otherwise remove unselected
# artifacts so Gradle cannot silently package a mixed-derivation ABI set.
CAN_REUSE_PREVIOUS=0
if [[ -f "$MANIFEST" ]]; then
  if python3 - "$MANIFEST" "$LOCK" "$ZIG_VER" "$OUT_DIR" "$LOCK_LIB_NAME" 2>/dev/null <<'PY'
import hashlib, json, sys
from pathlib import Path

manifest_path, lock_path, zig, out_dir, lib_name = sys.argv[1:6]
manifest = json.load(open(manifest_path))
lock = json.load(open(lock_path))
release = lock["ghostty"]["release"]

assert manifest.get("schema_version") == 2
assert manifest.get("ghostty_version") == lock["ghostty"]["version"]
assert manifest.get("source_sha256") == release["source"]["sha256"]
assert manifest.get("deps_sha256") == release["deps"]["sha256"]
assert manifest.get("zig_version") == zig
assert manifest.get("provenance") == "signed_release"
assert manifest.get("release_grade") is True

contracts = {entry["android_abi"]: entry for entry in lock["abis"]}
seen = set()
for entry in manifest.get("libraries") or []:
    abi = entry["android_abi"]
    assert abi not in seen
    seen.add(abi)
    contract = contracts[abi]
    assert entry.get("zig_target") == contract["zig_target"]
    assert entry.get("elf_machine_code") == contract["elf_machine_code"]
    if "android_api" in entry:
        assert entry["android_api"] == lock["android"]["min_api"]
    artifact = Path(out_dir) / abi / lib_name
    assert artifact.is_file()
    assert hashlib.sha256(artifact.read_bytes()).hexdigest() == entry.get("sha256")
for abi in contracts:
    artifact = Path(out_dir) / abi / lib_name
    assert not artifact.is_file() or abi in seen
PY
  then
    CAN_REUSE_PREVIOUS=1
  fi
fi

declare -A EXISTING_SUMS=()
if [[ $CAN_REUSE_PREVIOUS -eq 1 ]]; then
  while read -r digest rel; do
    EXISTING_SUMS["$rel"]="$digest"
  done < <(python3 - "$MANIFEST" "$LOCK_LIB_NAME" <<'PY'
import json, sys
manifest = json.load(open(sys.argv[1]))
lib_name = sys.argv[2]
for entry in manifest.get("libraries") or []:
    print(entry["sha256"], f"{entry['android_abi']}/{lib_name}")
PY
  )
else
  if [[ -f "$MANIFEST" || -f "$SUMS_FILE" ]]; then
    echo "Existing native outputs are not reusable for this source derivation."
  fi
  for android_abi in "${ALLOWED_ABIS[@]}"; do
    if ! need_abi "$android_abi"; then
      rm -f "$OUT_DIR/$android_abi/$LOCK_LIB_NAME"
    fi
  done
fi

if [[ $CAN_REUSE_PREVIOUS -eq 1 ]]; then
  for android_abi in "${ALLOWED_ABIS[@]}"; do
    if ! need_abi "$android_abi" \
      && [[ -f "$OUT_DIR/$android_abi/$LOCK_LIB_NAME" ]]; then
      "$ROOT/scripts/verify-android-native-symbols.py" \
        --lock "$LOCK" "$OUT_DIR/$android_abi/$LOCK_LIB_NAME"
    fi
  done
fi

BUILT_LIBS_JSON="[]"
built_any=0

for ((i = 0; i < LOCK_ABI_COUNT; i++)); do
  eval "android_abi=\$LOCK_ABI_${i}_ANDROID"
  eval "zig_target=\$LOCK_ABI_${i}_ZIG"
  eval "elf_code=\$LOCK_ABI_${i}_ELF"

  if ! need_abi "$android_abi"; then
    continue
  fi
  built_any=1

  echo ""
  echo "=== $android_abi ($zig_target) ==="
  # A fresh, empty global cache per build: with --system it proves the build
  # used only the packages of the dependency tarball (and Zig 0.17 can crash on
  # a cache that holds a package archive).
  ZIG_CACHE_DIR="$(mktemp -d "$BUILD_TMP_ROOT/libghostty-zigcache.XXXXXX")"
  PREFIX="$BUILD_WORK/prefix-$android_abi"
  (
    cd "$SOURCE_DIR"
    ZIG_GLOBAL_CACHE_DIR="$ZIG_CACHE_DIR" "$ZIG_BIN" build \
      --system "$DEPS_DIR" \
      --prefix "$PREFIX" \
      -Doptimize=ReleaseFast "-Dtarget=$zig_target"
  )
  rm -rf -- "$ZIG_CACHE_DIR"
  ZIG_CACHE_DIR=""

  mkdir -p "$OUT_DIR/$android_abi"
  src_so=""
  if compgen -G "$PREFIX/lib/libghostty-vt.so*" >/dev/null; then
    # Prefer a real file over a symlink.
    for candidate in "$PREFIX"/lib/libghostty-vt.so*; do
      if [[ -f "$candidate" && ! -L "$candidate" ]]; then
        src_so="$candidate"
        break
      fi
    done
    if [[ -z "$src_so" && -e "$PREFIX/lib/libghostty-vt.so" ]]; then
      src_so="$(readlink -f "$PREFIX/lib/libghostty-vt.so")"
    fi
  fi
  if [[ -z "${src_so:-}" || ! -f "$src_so" ]]; then
    echo "error: built library not found under $PREFIX/lib" >&2
    ls -la "$PREFIX/lib" >&2 || true
    exit 1
  fi

  dest="$OUT_DIR/$android_abi/$LOCK_LIB_NAME"
  cp -f "$src_so" "$dest"
  chmod 755 "$dest"

  machine_dec="$(python3 - "$dest" <<'PY'
import struct, sys
path = sys.argv[1]
with open(path, "rb") as f:
    data = f.read(64)
if data[:4] != b"\x7fELF":
    raise SystemExit("not ELF")
print(struct.unpack_from("<H", data, 18)[0])
PY
)"
  if [[ "$machine_dec" != "$elf_code" ]]; then
    echo "error: ELF machine $machine_dec != expected $elf_code for $android_abi" >&2
    exit 1
  fi

  # Android API level from .note.android.ident when present
  api_level="$(python3 - "$dest" <<'PY'
import struct, sys
data = open(sys.argv[1], "rb").read()
# Scan for "Android" note name
needle = b"Android\x00"
idx = data.find(needle)
if idx < 0:
    print("")
    raise SystemExit(0)
# description typically follows 64-bit aligned name; API is first u32 of desc
# Back up to note header is fragile; parse via known layout after name:
# After "Android\0" pad to 4, then desc: api u32, ndk name...
pad = (4 - ((idx + len(needle)) % 4)) % 4
desc_off = idx + len(needle) + pad
if desc_off + 4 <= len(data):
    print(struct.unpack_from("<I", data, desc_off)[0])
else:
    print("")
PY
)"
  if [[ -n "$api_level" && "$api_level" != "$LOCK_MIN_API" ]]; then
    echo "error: Android API $api_level != lock min_api $LOCK_MIN_API for $android_abi" >&2
    exit 1
  fi
  if [[ -n "$api_level" ]]; then
    echo "  android API note: $api_level"
  fi

  "$ROOT/scripts/verify-android-native-symbols.py" --lock "$LOCK" "$dest"

  digest="$(sha256sum "$dest" | awk '{print $1}')"
  rel="${android_abi}/${LOCK_LIB_NAME}"
  EXISTING_SUMS["$rel"]="$digest"
  size="$(wc -c <"$dest" | tr -d ' ')"
  echo "  -> $dest ($size bytes, sha256=${digest:0:12}…)"

  BUILT_LIBS_JSON="$(python3 - "$BUILT_LIBS_JSON" "$android_abi" "$zig_target" "$elf_code" "$digest" "$size" "${api_level:-}" <<'PY'
import json, sys
libs = json.loads(sys.argv[1])
entry = {
    "android_abi": sys.argv[2],
    "zig_target": sys.argv[3],
    "elf_machine_code": int(sys.argv[4]),
    "sha256": sys.argv[5],
    "bytes": int(sys.argv[6]),
}
if sys.argv[7]:
    entry["android_api"] = int(sys.argv[7])
libs.append(entry)
print(json.dumps(libs))
PY
)"
done

if [[ $built_any -eq 0 ]]; then
  echo "error: no ABIs selected to build" >&2
  exit 1
fi

# Write merged checksums (stable sorted order)
: >"$SUMS_FILE"
while IFS= read -r rel; do
  echo "${EXISTING_SUMS[$rel]}  $rel" >>"$SUMS_FILE"
done < <(printf '%s\n' "${!EXISTING_SUMS[@]}" | LC_ALL=C sort)

# Headers for JNI bridge: the tarball's include/ghostty, digest-checked again
# on the copy so a partial or edited copy cannot pass.
HEADER_PARENT="$(dirname "$HEADER_DST")"
mkdir -p "$HEADER_PARENT"
rm -rf "$HEADER_DST"
cp -a "$SOURCE_DIR/include/ghostty" "$HEADER_DST"
ACTUAL_HEADERS_SHA256="$(python3 "$ROOT/scripts/lib/ghostty_release.py" "$LOCK" headers-digest "$HEADER_DST")"
if [[ "$ACTUAL_HEADERS_SHA256" != "$LOCK_HEADERS_SHA256" ]]; then
  echo "error: copied headers digest $ACTUAL_HEADERS_SHA256 != lock $LOCK_HEADERS_SHA256" >&2
  exit 1
fi

cp -f "$NOTICE_SRC" "$OUT_DIR/GHOSTTY-MIT.txt"

python3 - "$MANIFEST" "$ZIG_VER" "$BUILT_LIBS_JSON" "$LOCK" "$SIGNATURE_VERIFIED" \
  "$CAN_REUSE_PREVIOUS" <<'PY'
import json, sys
path, zig, built_json, lock_path, signature_verified, reuse_previous = sys.argv[1:7]
lock = json.load(open(lock_path))
ghostty = lock["ghostty"]
# Merge libraries in manifest: replace ABIs we just built, keep others if present.
prev_libs = []
if reuse_previous == "1":
    try:
        prev = json.load(open(path))
        prev_libs = prev.get("libraries") or []
    except Exception:
        pass
built = json.loads(built_json)
by_abi = {e["android_abi"]: e for e in prev_libs}
for e in built:
    by_abi[e["android_abi"]] = e
libraries = [by_abi[k] for k in sorted(by_abi)]
manifest = {
    "schema_version": 2,
    "ghostty_version": ghostty["version"],
    "ghostty_tag": ghostty["tag"],
    "ghostty_commit": ghostty["commit"],
    "source_sha256": ghostty["release"]["source"]["sha256"],
    "deps_sha256": ghostty["release"]["deps"]["sha256"],
    "signature_verified": signature_verified == "True",
    "zig_version": zig,
    "provenance": "signed_release",
    "release_grade": True,
    "libraries": libraries,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2, sort_keys=True)
    f.write("\n")
print(f"Wrote {path} (release_grade={manifest['release_grade']})")
PY

echo ""
echo "Checksums: $SUMS_FILE"
echo "Headers:   $HEADER_DST"
echo "Done. Run: ./scripts/verify-libghostty.sh"
