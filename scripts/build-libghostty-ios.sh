#!/usr/bin/env bash
# Build the pinned libghostty-vt XCFramework for arm64 iOS devices and
# Apple Silicon iOS Simulator from the signed vt-X.Y.Z release tarball (see
# scripts/build-libghostty.sh). The output is consumed by TerminalVt.podspec.
#
#   MEWLA_GHOSTTY_RELEASE_DIR=/dir/with/tarballs ./scripts/build-libghostty-ios.sh  # offline

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/terminal-vt/native.lock.json"
OUT_DIR="$ROOT/app/modules/terminal-vt/libs/ios"
NOTICE_SRC="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"
TOOL_CACHE="${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}"
GHOSTTY_CACHE="${MEWLA_GHOSTTY_CACHE:-$ROOT/.cache/mewla/ghostty}"
BUILD_TMP_ROOT="${MEWLA_BUILD_TMPDIR:-${TMPDIR:-/tmp}}"

eval "$(python3 - "$LOCK" <<'PY'
import json, shlex, sys
lock = json.load(open(sys.argv[1]))
values = {
    "GHOSTTY_VERSION": lock["ghostty"]["version"],
    "GHOSTTY_TAG": lock["ghostty"]["tag"],
    "XCFRAMEWORK_NAME": lock["ios"]["xcframework_name"],
    "UPSTREAM_XCFRAMEWORK_NAME": lock["ios"]["upstream_xcframework_name"],
}
for key, value in values.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
)"

if ! xcodebuild -version >/dev/null 2>&1; then
  echo "error: full Xcode is required; install Xcode and select it with:" >&2
  echo "       sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer" >&2
  exit 1
fi

# shellcheck source=lib/zig.sh
source "$ROOT/scripts/lib/zig.sh"
# shellcheck source=lib/zig-xcode-compat.sh
source "$ROOT/scripts/lib/zig-xcode-compat.sh"
ZIG_BIN="$(mewla_resolve_zig "$LOCK" "$TOOL_CACHE")"

# The libSystem SDK clone (mewla_prepare_macos_sdk_for_zig) with
# scripts/zig-xcode-compat/xcrun is a workaround that
# scripts/check-zig-xcode-compat.sh (run in native-cache.yml) retires once the
# Xcode SDK no longer needs it.
MACOS_SDKROOT="$(mewla_prepare_macos_sdk_for_zig "$TOOL_CACHE")" || exit 1

BUILD_WORK="$(mktemp -d "$BUILD_TMP_ROOT/libghostty-ios.XXXXXX")"
ZIG_CACHE_DIR="$(mktemp -d "$BUILD_TMP_ROOT/libghostty-ios-zigcache.XXXXXX")"
trap 'rm -rf -- "$BUILD_WORK" "$ZIG_CACHE_DIR"' EXIT

RELEASE_JSON="$(python3 "$ROOT/scripts/lib/ghostty_release.py" "$LOCK" prepare \
  --cache "$GHOSTTY_CACHE/$GHOSTTY_TAG" \
  --dest "$BUILD_WORK/release" \
  ${MEWLA_GHOSTTY_RELEASE_DIR:+--release-dir "$MEWLA_GHOSTTY_RELEASE_DIR"})"
release_field() {
  python3 -c 'import json,sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$RELEASE_JSON" "$1"
}
GHOSTTY_SRC="$(release_field source_dir)"
DEPS_DIR="$(release_field deps_dir)"
SIGNATURE_VERIFIED="$(release_field signature_verified)"

echo "Building libghostty-vt XCFramework"
echo "  ghostty: $GHOSTTY_TAG (minisign verified: $SIGNATURE_VERIFIED)"
echo "  zig:     $($ZIG_BIN version)"

# The libghostty-vt tarball builds the library on its own (LIB_VERSION marks
# it); an XCFramework is opt-in because it needs Xcode. A fresh Zig global cache
# proves the build used only the packages of the dependency tarball.
(
  cd "$GHOSTTY_SRC"
  PATH="$ROOT/scripts/zig-xcode-compat:$PATH" \
    MEWLA_MACOS_SDKROOT="$MACOS_SDKROOT" \
    ZIG_GLOBAL_CACHE_DIR="$ZIG_CACHE_DIR" \
    "$ZIG_BIN" build --system "$DEPS_DIR" -Demit-xcframework=true -Doptimize=ReleaseFast
)

SOURCE="$GHOSTTY_SRC/zig-out/lib/$UPSTREAM_XCFRAMEWORK_NAME"
DESTINATION="$OUT_DIR/$XCFRAMEWORK_NAME"
if [[ ! -d "$SOURCE" ]]; then
  echo "error: expected XCFramework was not generated: $SOURCE" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
rm -rf "$DESTINATION"
ditto "$SOURCE" "$DESTINATION"
# The 1.0 XCFramework also carries a macOS slice whose archive has another name
# (libghostty-vt.a vs libghostty-vt-fat.a); CocoaPods refuses a vendored
# XCFramework with differing binary names. Mewla ships iOS device and Simulator
# only, so keep exactly those slices.
python3 - "$DESTINATION" <<'PY'
import plistlib, shutil, sys
from pathlib import Path

framework = Path(sys.argv[1])
plist_path = framework / "Info.plist"
plist = plistlib.loads(plist_path.read_bytes())
kept = []
for item in plist["AvailableLibraries"]:
    if item.get("SupportedPlatform") == "ios":
        kept.append(item)
    else:
        shutil.rmtree(framework / item["LibraryIdentifier"])
assert kept, "no iOS slices left in the XCFramework"
plist["AvailableLibraries"] = kept
plist_path.write_bytes(plistlib.dumps(plist))
PY
cp "$NOTICE_SRC" "$OUT_DIR/GHOSTTY-MIT.txt"

(
  cd "$OUT_DIR"
  find "$XCFRAMEWORK_NAME" -type f -print0 \
    | sort -z \
    | xargs -0 shasum -a 256 > SHA256SUMS
)

python3 - "$LOCK" "$DESTINATION" "$OUT_DIR/build-manifest.json" "$SIGNATURE_VERIFIED" <<'PY'
import json, pathlib, platform, subprocess, sys
lock = json.load(open(sys.argv[1]))
framework = pathlib.Path(sys.argv[2])
ghostty = lock["ghostty"]
manifest = {
    "schema_version": 2,
    "ghostty_version": ghostty["version"],
    "ghostty_tag": ghostty["tag"],
    "ghostty_commit": ghostty["commit"],
    "source_sha256": ghostty["release"]["source"]["sha256"],
    "deps_sha256": ghostty["release"]["deps"]["sha256"],
    "signature_verified": sys.argv[4] == "True",
    "zig_version": lock["zig"]["version"],
    "host": platform.platform(),
    "xcframework": framework.name,
    "files": sum(1 for path in framework.rglob("*") if path.is_file()),
    "release_grade": True,
}
pathlib.Path(sys.argv[3]).write_text(json.dumps(manifest, indent=2) + "\n")
PY

"$ROOT/scripts/verify-libghostty-ios.sh"
echo "iOS libghostty-vt build complete: $DESTINATION"
