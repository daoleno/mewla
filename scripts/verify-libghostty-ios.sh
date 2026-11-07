#!/usr/bin/env bash
# Verify that the pinned iOS libghostty-vt XCFramework contains both the
# arm64 device and Apple Silicon Simulator slices required by Mewla.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/terminal-vt/native.lock.json"
OUT_DIR="$ROOT/app/modules/terminal-vt/libs/ios"

eval "$(python3 - "$LOCK" <<'PY'
import json, shlex, sys
lock = json.load(open(sys.argv[1]))
for key, value in {
    "XCFRAMEWORK_NAME": lock["ios"]["xcframework_name"],
}.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
)"

FRAMEWORK="$OUT_DIR/$XCFRAMEWORK_NAME"
PLIST="$FRAMEWORK/Info.plist"
SUMS="$OUT_DIR/SHA256SUMS"
MANIFEST="$OUT_DIR/build-manifest.json"

[[ -f "$PLIST" ]] || { echo "FAIL: missing $PLIST" >&2; exit 1; }
[[ -f "$SUMS" ]] || { echo "FAIL: missing $SUMS" >&2; exit 1; }
[[ -f "$MANIFEST" ]] || { echo "FAIL: missing $MANIFEST" >&2; exit 1; }
[[ -f "$OUT_DIR/GHOSTTY-MIT.txt" ]] || { echo "FAIL: missing Ghostty notice" >&2; exit 1; }

python3 - "$PLIST" <<'PY'
import plistlib, sys
with open(sys.argv[1], "rb") as stream:
    plist = plistlib.load(stream)
slices = plist.get("AvailableLibraries", [])
device = any(
    item.get("SupportedPlatform") == "ios"
    and not item.get("SupportedPlatformVariant")
    and "arm64" in item.get("SupportedArchitectures", [])
    for item in slices
)
simulator = any(
    item.get("SupportedPlatform") == "ios"
    and item.get("SupportedPlatformVariant") == "simulator"
    and "arm64" in item.get("SupportedArchitectures", [])
    for item in slices
)
assert device, "missing arm64 iOS device slice"
assert simulator, "missing arm64 iOS Simulator slice"
print("ok: XCFramework contains arm64 iOS device and Simulator slices")
PY

(
  cd "$OUT_DIR"
  shasum -a 256 -c SHA256SUMS
)

python3 - "$MANIFEST" "$LOCK" <<'PY'
import json, sys
manifest = json.load(open(sys.argv[1]))
lock = json.load(open(sys.argv[2]))
ghostty = lock["ghostty"]
assert manifest["schema_version"] == 2
for key, want in (
    ("ghostty_version", ghostty["version"]),
    ("ghostty_tag", ghostty["tag"]),
    ("ghostty_commit", ghostty["commit"]),
    ("source_sha256", ghostty["release"]["source"]["sha256"]),
    ("deps_sha256", ghostty["release"]["deps"]["sha256"]),
):
    assert manifest[key] == want, f"manifest {key} is {manifest[key]!r}, the lock has {want!r}"
assert manifest["release_grade"] is True
# The framework was built with exactly the Zig lib patches the lock names.
expected = [
    {"path": patch["path"], "sha256": patch["sha256"]}
    for patch in lock["zig"].get("lib_patches") or []
]
assert manifest.get("zig_lib_patches") == expected, (
    f"zig_lib_patches are {manifest.get('zig_lib_patches')!r}, the lock names {expected!r}"
)
print("ok: manifest matches the pinned Ghostty release and Zig lib patches")
PY

# Every slice carries the headers of the pinned release: the bridge is compiled
# against them, so a slice from another version would break at run time.
for headers in "$FRAMEWORK"/*/Headers/ghostty; do
  actual="$(python3 "$ROOT/scripts/lib/ghostty_release.py" "$LOCK" headers-digest "$headers")"
  want="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['ghostty']['headers_sha256'])" "$LOCK")"
  [[ "$actual" == "$want" ]] \
    || { echo "FAIL: $headers digest $actual != lock $want" >&2; exit 1; }
done
echo "ok: every XCFramework slice carries the pinned headers"

find "$FRAMEWORK" -path '*/Headers/ghostty/vt.h' -print -quit | grep -q . \
  || { echo "FAIL: XCFramework is missing ghostty/vt.h" >&2; exit 1; }
find "$FRAMEWORK" -path '*/Headers/module.modulemap' -print -quit | grep -q . \
  || { echo "FAIL: XCFramework is missing module.modulemap" >&2; exit 1; }

echo "iOS libghostty-vt verification passed"
