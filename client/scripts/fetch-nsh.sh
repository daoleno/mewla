#!/usr/bin/env bash
# Installs the pinned NSH release into $NSH_HOME (default ~/.nsh): the CLI (it carries this machine's
# runtime and the web runtime) and the Android AARs the app embeds ($NSH_HOME/embed/android); with --ios,
# NativeShell(.Dev).xcframework into client/ios/Frameworks. Checksums are pinned here: nothing is
# installed that was not checked (NSH 0.5 also adds other platforms' runtimes on demand; the app's
# builds use none of them).
set -euo pipefail
version="v0.6.0"
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
    Linux-x86_64) cli=$(fetch nsh-linux-x86_64.tar.gz 19d360eb8dd1ba8847a3b3ad06082f6f3705eb04ae8178357a8cdad31880f0d9) ;;
    Darwin-arm64) cli=$(fetch nsh-macos-arm64.tar.gz b2a8370c8b44ca6059d2d8665b48b08472a4fee3956813b99223f4e3ce5491d9) ;;
    *) echo "no NSH $version CLI for $(uname -sm)" >&2; exit 1 ;;
  esac
  # Never replaces someone's own NSH: an existing $NSH_HOME of another version is an error.
  [[ ! -e "$home" ]] || { echo "$home holds another NSH; set NSH_HOME to a new directory" >&2; exit 1; }
  tar -xzf "$cli" -C "$work"
  mkdir -p "$(dirname "$home")" && mv "$work/nsh" "$home"
fi

# The Android AARs (Gradle reads $NSH_HOME/embed/android/nsh(-dev).aar); since 0.5.0 the CLI archive no longer carries them.
if [[ ! -f "$home/embed/android/.$version" ]]; then
  embed=$(fetch nsh-embed-android.zip fc59ffa013fc43265924a0e55cca58bb66e17e1c011b2d640e7cd43fb7c4a492)
  unzip -q "$embed" -d "$work/android"
  rm -rf "$home/embed/android" && mkdir -p "$home/embed/android"
  mv "$work/android/nsh-embed-android/nsh.aar" "$work/android/nsh-embed-android/nsh-dev.aar" "$home/embed/android/"
  touch "$home/embed/android/.$version"
fi

if [[ "${1:-}" == "--ios" ]]; then
  embed=$(fetch nsh-embed-ios.zip 5d34b2feaee92c813eb12afe3d928f346a020988531efaf94b5d4d124f0b0afd)
  unzip -q "$embed" -d "$work/ios"
  rm -rf "$client/ios/Frameworks" && mkdir -p "$client/ios/Frameworks"
  for framework in NativeShell NativeShellDev; do
    found="$(find "$work/ios" -name "$framework.xcframework" -type d | head -1)"
    [[ -n "$found" ]] && mv "$found" "$client/ios/Frameworks/"
  done
  [[ -d "$client/ios/Frameworks/NativeShell.xcframework" ]] || { echo "nsh-embed-ios.zip holds no NativeShell.xcframework" >&2; exit 1; }
fi
"$home/bin/nsh" --version
