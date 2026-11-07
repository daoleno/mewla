#!/usr/bin/env bash
# Fail when the Zig/Xcode workaround is no longer needed, so it gets deleted
# instead of living on. Run on a macOS runner with the Xcode the iOS build uses.
# The workaround is the libSystem SDK clone (see scripts/lib/zig-xcode-compat.sh).
# Exit 0: still needed. Exit 1: retired, delete it. Exit 2: a layout changed and
# the workaround's premise needs a fresh look.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/zig-xcode-compat.sh
source "$ROOT/scripts/lib/zig-xcode-compat.sh"

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
    - mewla_libsystem_arm64_support in scripts/lib/zig-xcode-compat.sh, and with it
      this check, its test, and the native-cache.yml job that runs it
    - the scripts/zig-xcode-compat/** and scripts/lib/zig-xcode-compat.sh
      cache-key entries in .github/actions/ios-native/action.yml
MESSAGE
    exit 1
    ;;
  *)
    echo "error: cannot read the arm64 targets of libSystem in $sdk; its layout changed." >&2
    echo "       Re-read prepare_macos_sdk_for_zig in scripts/lib/zig-xcode-compat.sh." >&2
    exit 2
    ;;
esac
