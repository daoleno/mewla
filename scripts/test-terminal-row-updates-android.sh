#!/usr/bin/env bash
# Exercise the shared native formatter against a real Android Ghostty library.
# Requires an already running, explicitly selected device; never starts one.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SERIAL="${1:?usage: test-terminal-row-updates-android.sh DEVICE_SERIAL}"
SDK="${ANDROID_HOME:?ANDROID_HOME is required}"
BUILD_TMP="${ZEN_BUILD_TMPDIR:-${TMPDIR:-/tmp}}/terminal-row-updates-test"
MODULE="$ROOT/app/modules/zen-terminal-vt"
ABI="$(adb -s "$SERIAL" shell getprop ro.product.cpu.abi | tr -d '\r')"
case "$ABI" in
  x86_64) TARGET=x86_64-linux-android29 ;;
  arm64-v8a) TARGET=aarch64-linux-android29 ;;
  *) echo "Unsupported device ABI: $ABI" >&2; exit 1 ;;
esac
NDK="${ANDROID_NDK_HOME:-$SDK/ndk/27.1.12297006}"
mkdir -p "$BUILD_TMP"
"$NDK/toolchains/llvm/prebuilt/linux-x86_64/bin/$TARGET-clang++" \
  -std=c++17 -UNDEBUG -static-libstdc++ \
  -I "$MODULE/android/src/main/cpp" -L "$MODULE/libs/android/$ABI" \
  "$MODULE/tests/render_row_updates.cpp" -lghostty_vt -o "$BUILD_TMP/check"
DEVICE_DIR="/data/local/tmp/zen-row-updates-$$"
trap 'adb -s "$SERIAL" shell rm -rf "$DEVICE_DIR" >/dev/null 2>&1 || true; rm -f "$BUILD_TMP/check"; rmdir "$BUILD_TMP" 2>/dev/null || true' EXIT
adb -s "$SERIAL" shell mkdir -p "$DEVICE_DIR"
adb -s "$SERIAL" push "$BUILD_TMP/check" "$DEVICE_DIR/" >/dev/null
adb -s "$SERIAL" push "$MODULE/libs/android/$ABI/libghostty_vt.so" "$DEVICE_DIR/libghostty-vt.so.0" >/dev/null
adb -s "$SERIAL" shell "chmod 700 '$DEVICE_DIR/check' && LD_LIBRARY_PATH='$DEVICE_DIR' '$DEVICE_DIR/check'"
