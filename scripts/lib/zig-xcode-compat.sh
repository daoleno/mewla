# shellcheck shell=bash
# The Zig/Xcode workaround for the iOS native build.
#
# Xcode 26.6's macOS 26.5 SDK lists the top-level libSystem stub as arm64e
# only, while Zig 0.15.2 requires an exact arm64 match. build-libghostty-ios.sh
# therefore builds against a patched SDK clone and scripts/zig-xcode-compat/xcrun
# points Zig at it. Both exist only for as long as that is true;
# scripts/check-zig-xcode-compat.sh fails when it no longer is.
#
# mewla_libsystem_arm64_support SDK prints one word:
#   native       the SDK already lists arm64-macos; no workaround is needed
#   arm64e-only  the SDK lists only arm64e; the workaround is needed
#   unknown      the stub is missing or its layout changed

mewla_libsystem_arm64_support() {
  local libsystem="$1/usr/lib/libSystem.B.tbd"
  local targets
  if [[ ! -r "$libsystem" ]] || ! targets="$(grep -m1 '^targets:' "$libsystem")"; then
    echo unknown
  elif grep -q 'arm64-macos' <<<"$targets"; then
    echo native
  elif grep -q 'arm64e-macos' <<<"$targets"; then
    echo arm64e-only
  else
    echo unknown
  fi
}
