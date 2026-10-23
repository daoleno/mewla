#!/usr/bin/env bash
# Fail when the Zig/Xcode workaround is no longer needed, so it gets deleted
# instead of living on. Run on a macOS runner with the Xcode the iOS build uses.
# Exit 0: still needed. Exit 1: retired, delete the workaround. Exit 2: the SDK
# layout changed and the workaround's premise needs a fresh look.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/zig-xcode-compat.sh
source "$ROOT/scripts/lib/zig-xcode-compat.sh"

sdk="$(realpath "$(xcrun --sdk macosx --show-sdk-path)")"
case "$(mewla_libsystem_arm64_support "$sdk")" in
  arm64e-only)
    echo "The Zig/Xcode workaround is still needed: $sdk lists libSystem as arm64e only."
    ;;
  native)
    cat >&2 <<MESSAGE
error: the Zig/Xcode workaround is retired.
  $sdk
  lists arm64-macos in usr/lib/libSystem.B.tbd, which is what Zig requires.
  Delete it:
    - scripts/zig-xcode-compat/
    - prepare_macos_sdk_for_zig and its use in scripts/build-libghostty-ios.sh
      (build against the SDK from xcrun directly)
    - scripts/lib/zig-xcode-compat.sh and this check, and the step that runs it
      in .github/workflows/native-cache.yml
    - the scripts/zig-xcode-compat/** entries in .github/workflows/native-cache.yml
      and .github/actions/ios-native/action.yml
MESSAGE
    exit 1
    ;;
  *)
    echo "error: cannot read the arm64 targets of libSystem in $sdk; its layout changed." >&2
    echo "       Re-read prepare_macos_sdk_for_zig in scripts/build-libghostty-ios.sh." >&2
    exit 2
    ;;
esac
