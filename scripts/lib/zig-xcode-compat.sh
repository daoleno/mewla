# shellcheck shell=bash
# The Zig/Xcode workaround for the iOS native build: libSystem. Xcode's macOS
# SDK lists the top-level libSystem stub as arm64e only, while Zig requires an
# exact arm64 match. build-libghostty-ios.sh therefore builds against a patched
# SDK clone and scripts/zig-xcode-compat/xcrun points Zig at it.
# scripts/check-zig-xcode-compat.sh retires it once Xcode no longer needs it.
#
# (A second workaround, a patch for the INFINITY declaration in Zig 0.15.2's
# bundled libcxx, was retired with the move to Zig 0.17.0: its libcxx builds
# against the Xcode 27 SDK as shipped.)
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

# mewla_prepare_macos_sdk_for_zig TOOL_CACHE prints the macOS SDK Zig should
# use (the build sets MEWLA_MACOS_SDKROOT to it for scripts/zig-xcode-compat/xcrun):
# the SDK xcrun names when its libSystem stub lists arm64, otherwise a
# copy-on-write clone of it with arm64 added. Xcode itself is never touched.
mewla_prepare_macos_sdk_for_zig() {
  local tool_cache="$1" sdk
  sdk="$(realpath "$(xcrun --sdk macosx --show-sdk-path)")"
  local libsystem="$sdk/usr/lib/libSystem.B.tbd"

  case "$(mewla_libsystem_arm64_support "$sdk")" in
    native)
      echo "$sdk"
      return
      ;;
    arm64e-only) ;;
    *)
      echo "error: unsupported libSystem target list in $libsystem" >&2
      return 1
      ;;
  esac

  # Xcode 26.6's macOS 26.5 SDK advertises the top-level libSystem stub as
  # arm64e-only. Apple's linker accepts it for arm64 hosts, while Zig 0.15.2
  # intentionally requires an exact target match. Give Zig a clone with arm64
  # added to those target lists.
  local xcode_build
  xcode_build="$(xcodebuild -version | awk '/Build version/ {print $3}')"
  local compat_sdk="$tool_cache/macos-sdk-$xcode_build-arm64"
  local marker="$compat_sdk/.zig-arm64-libsystem"
  if [[ ! -f "$marker" ]]; then
    echo "Preparing Zig-compatible macOS SDK clone for Xcode $xcode_build..." >&2
    rm -rf "$compat_sdk"
    mkdir -p "$(dirname "$compat_sdk")"
    if [[ "$(uname -s)" == Darwin ]]; then
      cp -cR "$sdk" "$compat_sdk"
    else
      cp -R "$sdk" "$compat_sdk"
    fi
    python3 - "$compat_sdk/usr/lib/libSystem.B.tbd" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text()
text = text.replace("arm64e-macos", "arm64-macos, arm64e-macos")
text = text.replace("arm64e-maccatalyst", "arm64-maccatalyst, arm64e-maccatalyst")
path.write_text(text)
PY
    touch "$marker"
  fi
  echo "$compat_sdk"
}
