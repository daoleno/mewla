#!/usr/bin/env bash
# Installs the pinned NSH release into $NSH_HOME (default ~/.nsh): the CLI (it carries this machine's
# runtime and the web runtime) and the Android AARs the app embeds ($NSH_HOME/embed/android); with --ios,
# NativeShell(.Dev).xcframework into client/ios/Frameworks. Checksums are pinned here: nothing is
# installed that was not checked (NSH 0.5 also adds other platforms' runtimes on demand; the app's
# builds use none of them).
set -euo pipefail
version="v0.7.0"
base="https://github.com/longbridge/nsh/releases/download/$version"
home="${NSH_HOME:-$HOME/.nsh}"
client="$(cd "$(dirname "$0")/.." && pwd)"

fetch() { # asset sha256 -> path of the verified download
  local file="$work/$1"
  curl -fsSL --retry 3 -o "$file" "$base/$1"
  [[ "$(shasum -a 256 "$file" | cut -d' ' -f1)" == "$2" ]] || { echo "checksum of $1 does not match" >&2; exit 1; }
  echo "$file"
}
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [[ "$("$home/bin/nsh" --version 2>/dev/null | cut -d' ' -f2)" != "${version#v}" ]]; then
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) cli=$(fetch nsh-linux-x86_64.tar.gz d281bfa3d93d84e9831e1a97dfdb63104329fcc87a0a48c78db5aeabb00c0073) ;;
    Darwin-arm64) cli=$(fetch nsh-macos-arm64.tar.gz 5b2e5385e09a65ed63b178a8dd9cef03a9dcab204d2efc942300e0c5baea3853) ;;
    *) echo "no NSH $version CLI for $(uname -sm)" >&2; exit 1 ;;
  esac
  # Never replaces someone's own NSH: an existing $NSH_HOME of another version is an error.
  [[ ! -e "$home" ]] || { echo "$home holds another NSH; set NSH_HOME to a new directory" >&2; exit 1; }
  tar -xzf "$cli" -C "$work"
  mkdir -p "$(dirname "$home")" && mv "$work/nsh" "$home"
fi

# The Android AARs (Gradle reads $NSH_HOME/embed/android/nsh(-dev).aar); since 0.5.0 the CLI archive no longer carries them.
if [[ ! -f "$home/embed/android/.$version" ]]; then
  embed=$(fetch nsh-embed-android.zip 89d0c979d1a457b57118b32b5f8312e45c856377501816b37069ad9d36c77033)
  unzip -q "$embed" -d "$work/android"
  rm -rf "$home/embed/android" && mkdir -p "$home/embed/android"
  mv "$work/android/nsh-embed-android/nsh.aar" "$work/android/nsh-embed-android/nsh-dev.aar" "$home/embed/android/"
  touch "$home/embed/android/.$version"
fi

if [[ "${1:-}" == "--ios" ]]; then
  embed=$(fetch nsh-embed-ios.zip 71a952b23b8ac0d0a8a40176acd76149bd1a6e3007eca8bc8c4c66ca7f26124a)
  unzip -q "$embed" -d "$work/ios"
  rm -rf "$client/ios/Frameworks" && mkdir -p "$client/ios/Frameworks"
  for framework in NativeShell NativeShellDev; do
    found="$(find "$work/ios" -name "$framework.xcframework" -type d | head -1)"
    [[ -n "$found" ]] && mv "$found" "$client/ios/Frameworks/"
  done
  [[ -d "$client/ios/Frameworks/NativeShell.xcframework" ]] || { echo "nsh-embed-ios.zip holds no NativeShell.xcframework" >&2; exit 1; }
fi
"$home/bin/nsh" --version
