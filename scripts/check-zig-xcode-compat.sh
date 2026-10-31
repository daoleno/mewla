#!/usr/bin/env bash
# Fail when a Zig/Xcode workaround is no longer needed, so it gets deleted
# instead of living on. Run on a macOS runner with the Xcode the iOS build uses.
# There are two workarounds (see scripts/lib/zig-xcode-compat.sh) and each is
# checked and reported on its own, so retiring one never hides the other.
# Exit 0: both still needed. Exit 1: one is retired, delete it. Exit 2: a
# layout changed and the workaround's premise needs a fresh look.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/terminal-vt/native.lock.json"
TOOL_CACHE="${MEWLA_TOOL_CACHE:-$ROOT/.cache/mewla-tools}"
# shellcheck source=lib/zig.sh
source "$ROOT/scripts/lib/zig.sh"
# shellcheck source=lib/zig-xcode-compat.sh
source "$ROOT/scripts/lib/zig-xcode-compat.sh"

status=0
retired() { status=$(( status == 2 ? 2 : 1 )); }
changed() { status=2; }

sdk="$(realpath "$(xcrun --sdk macosx --show-sdk-path)")"
case "$(mewla_libsystem_arm64_support "$sdk")" in
  arm64e-only)
    echo "libSystem workaround: still needed; $sdk lists libSystem as arm64e only."
    ;;
  native)
    cat >&2 <<MESSAGE
error: the libSystem workaround is retired.
  $sdk
  lists arm64-macos in usr/lib/libSystem.B.tbd, which is what Zig requires.
  Delete it:
    - scripts/zig-xcode-compat/xcrun
    - prepare_macos_sdk_for_zig and its use in scripts/build-libghostty-ios.sh
      (build against the SDK from xcrun directly), and the PATH entry for
      scripts/zig-xcode-compat there
    - mewla_libsystem_arm64_support in scripts/lib/zig-xcode-compat.sh
    - the scripts/zig-xcode-compat/** cache-key entries in
      .github/actions/ios-native/action.yml, if nothing is left in that directory
MESSAGE
    retired
    ;;
  *)
    echo "error: cannot read the arm64 targets of libSystem in $sdk; its layout changed." >&2
    echo "       Re-read prepare_macos_sdk_for_zig in scripts/build-libghostty-ios.sh." >&2
    changed
    ;;
esac

ZIG_BIN="$(mewla_resolve_zig "$LOCK" "$TOOL_CACHE")"
probe_log="$(mktemp "${TMPDIR:-/tmp}/mewla-zig-probe-log.XXXXXX")"
export MEWLA_ZIG_PROBE_LOG="$probe_log"
# The probe builds libcxx the way the build does, so it gets the same SDK.
probe_sdk=""
if [[ "$(mewla_libsystem_arm64_support "$sdk")" != unknown ]]; then
  probe_sdk="$(mewla_prepare_macos_sdk_for_zig "$TOOL_CACHE")" || probe_sdk=""
fi
libcxx_state="$(mewla_zig_libcxx_support "$ZIG_BIN" "$probe_sdk")"
# What the stock header says, read from its text: unpatched (still the line the
# patch changes), applied (a hand patch) or mismatch (Zig changed the header).
stock_lib="$(mewla_zig_stock_lib "$ZIG_BIN")"
header_state=mismatch
while IFS= read -r patch_file; do
  [[ -n "$patch_file" ]] || continue
  header_state="$(mewla_zig_lib_patch_state "$stock_lib" "$patch_file")"
done < <(mewla_lock_lib_patches "$LOCK" "$ROOT")
echo "libcxx probe: build=$libcxx_state, stock header (${stock_lib:-?}) vs the lock's patch: $header_state"
# The probe's own account of what it built and which headers libcxx saw (see
# mewla_zig_libcxx_support): the libc file, one line per build, include dirs.
sed -n -e 's/^== variant /  libcxx probe variant /p' -e 's/^  libcxx include dir: /  libcxx include dir: /p' -e 's/^== \(libc file\|no libc file\)/  libcxx probe \1/p' "$probe_log"
if mewla_zig_lib_already_patched "$ZIG_BIN" "$LOCK" "$ROOT"; then
  libcxx_state=patched
elif [[ "$libcxx_state" == fixed && "$header_state" == unpatched ]]; then
  # The header still has the line the patch exists for, yet every probe
  # configuration built libcxx: the probe is not seeing what the build sees, so
  # it cannot call the workaround retired.
  libcxx_state=disagrees
fi
case "$libcxx_state" in
  patched)
    echo "error: $ZIG_BIN's own lib dir already carries the libcxx patch (a hand patch?), so the probe can't tell whether the workaround is needed." >&2
    echo "       Remove that Zig (let the check unpack the pinned one afresh), unset ZIG_LIB_DIR, and run the check again." >&2
    changed
    ;;
  disagrees)
    cat >&2 <<MESSAGE
error: the libcxx probe and Zig's header disagree.
  The stock header under $stock_lib still has the line
  scripts/zig-xcode-compat/ patches (bare INFINITY), but building libcxx for
  macOS with $sdk succeeded in every configuration the probe tries
  (the variant lines above). The probe is not reproducing the failing libcxx
  build, so the workaround cannot be called retired.
  Build the pinned Ghostty with the patch removed from the lock (or run
  "$ZIG_BIN" build on it with ZIG_LIB_DIR unset) and report whether libcxx
  fails; if it does, the probe's flags need to match the build's.
MESSAGE
    changed
    ;;
  broken)
    echo "libcxx workaround: still needed; Zig $("$ZIG_BIN" version)'s libcxx fails on INFINITY with $sdk."
    ;;
  fixed)
    cat >&2 <<MESSAGE
error: the libcxx workaround is retired.
  Zig $("$ZIG_BIN" version)'s own libcxx compiles for macOS with $sdk.
  Delete it:
    - the zig.lib_patches entry in app/modules/terminal-vt/native.lock.json
      and scripts/zig-xcode-compat/zig-*-libcxx-infinity.patch
    - mewla_patched_zig_lib and mewla_zig_libcxx_support in
      scripts/lib/zig-xcode-compat.sh, their use in scripts/build-libghostty-ios.sh
      (stop setting ZIG_LIB_DIR) and the zig_lib_patches checks in
      scripts/verify-libghostty-ios.sh
MESSAGE
    retired
    ;;
  *)
    echo "error: the libcxx probe failed in a way the workaround isn't for; its premise needs a fresh look:" >&2
    sed 's/^/  /' "$probe_log" >&2
    changed
    ;;
esac
rm -f "$probe_log"
exit "$status"
