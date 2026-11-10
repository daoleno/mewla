#!/usr/bin/env bash
# Installs the pinned NSH release: the CLI with the Android AARs (into $NSH_HOME, default ~/.nsh)
# and, with --ios, NativeShell(.Dev).xcframework into client/ios/Frameworks. Checksums are pinned here.
set -euo pipefail
version="v0.4.0"
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
    Linux-x86_64) cli=$(fetch nsh-linux-x86_64.tar.gz 8806121aebf40bb51f8fb6aa703722fbb01253f9a435759c18d53beece1ec976) ;;
    Darwin-arm64) cli=$(fetch nsh-macos-arm64.tar.gz f63c3d57c31b28c9b34227aac81f099d1e0e61f1db552a6ec5127d114eaf59cb) ;;
    *) echo "no NSH $version CLI for $(uname -sm)" >&2; exit 1 ;;
  esac
  # Never replaces someone's own NSH: an existing $NSH_HOME of another version is an error.
  [[ ! -e "$home" ]] || { echo "$home holds another NSH; set NSH_HOME to a new directory" >&2; exit 1; }
  tar -xzf "$cli" -C "$work"
  mkdir -p "$(dirname "$home")" && mv "$work/nsh" "$home"
fi

if [[ "${1:-}" == "--ios" ]]; then
  embed=$(fetch nsh-embed-ios.zip 698404af02006ae9bf9e2b2c7cd2fd89e52f6d65ab2cb8273229bc31394640c7)
  unzip -q "$embed" -d "$work/ios"
  rm -rf "$client/ios/Frameworks" && mkdir -p "$client/ios/Frameworks"
  for framework in NativeShell NativeShellDev; do
    found="$(find "$work/ios" -name "$framework.xcframework" -type d | head -1)"
    [[ -n "$found" ]] && mv "$found" "$client/ios/Frameworks/"
  done
  [[ -d "$client/ios/Frameworks/NativeShell.xcframework" ]] || { echo "nsh-embed-ios.zip holds no NativeShell.xcframework" >&2; exit 1; }
fi
"$home/bin/nsh" --version
