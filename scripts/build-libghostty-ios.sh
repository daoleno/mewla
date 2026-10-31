#!/usr/bin/env bash
# Build the pinned libghostty-vt XCFramework for arm64 iOS devices and
# Apple Silicon iOS Simulator. The output is consumed by TerminalVt.podspec.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/terminal-vt/native.lock.json"
OUT_DIR="$ROOT/app/modules/terminal-vt/libs/ios"
NOTICE_SRC="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"
TOOL_CACHE="${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}"
GHOSTTY_CACHE="${MEWLA_GHOSTTY_CACHE:-$ROOT/.cache/mewla/ghostty}"

eval "$(python3 - "$LOCK" <<'PY'
import json, shlex, sys
lock = json.load(open(sys.argv[1]))
values = {
    "GHOSTTY_REPO": lock["ghostty"]["repository"],
    "GHOSTTY_COMMIT": lock["ghostty"]["commit"],
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

# Two workarounds, each retired on its own once scripts/check-zig-xcode-compat.sh
# (run in native-cache.yml) fails because it is no longer needed: the libSystem
# SDK clone (mewla_prepare_macos_sdk_for_zig) with scripts/zig-xcode-compat/xcrun,
# and the patched Zig lib dir (the Xcode 27 SDK leaves INFINITY undeclared in
# Zig's libcxx).
MACOS_SDKROOT="$(mewla_prepare_macos_sdk_for_zig "$TOOL_CACHE")" || exit 1
ZIG_LIB="$(mewla_patched_zig_lib "$ZIG_BIN" "$LOCK" "$ROOT" "$TOOL_CACHE")"

GHOSTTY_SRC="${GHOSTTY_SRC:-$GHOSTTY_CACHE}"
if [[ ! -d "$GHOSTTY_SRC/.git" ]]; then
  mkdir -p "$(dirname "$GHOSTTY_SRC")"
  git clone --filter=blob:none "$GHOSTTY_REPO" "$GHOSTTY_SRC"
fi

CURRENT="$(git -C "$GHOSTTY_SRC" rev-parse HEAD)"
if [[ "$CURRENT" != "$GHOSTTY_COMMIT" ]]; then
  git -C "$GHOSTTY_SRC" fetch --depth=1 origin "$GHOSTTY_COMMIT" \
    || git -C "$GHOSTTY_SRC" fetch origin "$GHOSTTY_COMMIT"
  git -C "$GHOSTTY_SRC" checkout --detach "$GHOSTTY_COMMIT"
fi

if [[ -n "$(git -C "$GHOSTTY_SRC" status --porcelain)" ]]; then
  echo "error: Ghostty checkout is dirty: $GHOSTTY_SRC" >&2
  exit 1
fi

echo "Building libghostty-vt XCFramework"
echo "  ghostty: $GHOSTTY_SRC @ $GHOSTTY_COMMIT"
echo "  zig:     $($ZIG_BIN version) (lib $ZIG_LIB)"

(
  cd "$GHOSTTY_SRC"
  PATH="$ROOT/scripts/zig-xcode-compat:$PATH" \
    MEWLA_MACOS_SDKROOT="$MACOS_SDKROOT" \
    ZIG_LIB_DIR="$ZIG_LIB" \
    "$ZIG_BIN" build -Demit-lib-vt=true -Doptimize=ReleaseFast
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
cp "$NOTICE_SRC" "$OUT_DIR/GHOSTTY-MIT.txt"

(
  cd "$OUT_DIR"
  find "$XCFRAMEWORK_NAME" -type f -print0 \
    | sort -z \
    | xargs -0 shasum -a 256 > SHA256SUMS
)

python3 - "$LOCK" "$DESTINATION" "$OUT_DIR/build-manifest.json" <<'PY'
import json, pathlib, platform, subprocess, sys
lock = json.load(open(sys.argv[1]))
framework = pathlib.Path(sys.argv[2])
manifest = {
    "schema_version": 1,
    "ghostty_commit": lock["ghostty"]["commit"],
    "zig_version": lock["zig"]["version"],
    "zig_lib_patches": [
        {"path": patch["path"], "sha256": patch["sha256"]}
        for patch in lock["zig"].get("lib_patches") or []
    ],
    "host": platform.platform(),
    "xcframework": framework.name,
    "files": sum(1 for path in framework.rglob("*") if path.is_file()),
    "release_grade": True,
}
pathlib.Path(sys.argv[3]).write_text(json.dumps(manifest, indent=2) + "\n")
PY

"$ROOT/scripts/verify-libghostty-ios.sh"
echo "iOS libghostty-vt build complete: $DESTINATION"
