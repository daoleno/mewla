#!/usr/bin/env bash
# Installs the pinned NSH release into $NSH_HOME (default ~/.nsh): the CLI (it carries this machine's
# runtime and the web runtime) and the Android AARs the app embeds ($NSH_HOME/embed/android); with --ios,
# NativeShell(.Dev).xcframework into client/ios/Frameworks. Checksums are pinned here: nothing is
# installed that was not checked (NSH 0.5 also adds other platforms' runtimes on demand; the app's
# builds use none of them).
set -euo pipefail
version="v0.5.1"
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
    Linux-x86_64) cli=$(fetch nsh-linux-x86_64.tar.gz f34729e613196ca122d956deb543a92f319e053ff17dfc09ba04a6ec2fb79c74) ;;
    Darwin-arm64) cli=$(fetch nsh-macos-arm64.tar.gz 2710affd59e6345b6f87dfa1f579c36b9a9d7a58992b8399d14f39290da048a3) ;;
    *) echo "no NSH $version CLI for $(uname -sm)" >&2; exit 1 ;;
  esac
  # Never replaces someone's own NSH: an existing $NSH_HOME of another version is an error.
  [[ ! -e "$home" ]] || { echo "$home holds another NSH; set NSH_HOME to a new directory" >&2; exit 1; }
  tar -xzf "$cli" -C "$work"
  mkdir -p "$(dirname "$home")" && mv "$work/nsh" "$home"
fi

# The Android AARs (Gradle reads $NSH_HOME/embed/android/nsh(-dev).aar); since 0.5.0 the CLI archive no longer carries them.
if [[ ! -f "$home/embed/android/.$version" ]]; then
  embed=$(fetch nsh-embed-android.zip 38af836d7892b8ed449e72bd023529594d90229aa2f019c900e472dc668fb0ce)
  unzip -q "$embed" -d "$work/android"
  rm -rf "$home/embed/android" && mkdir -p "$home/embed/android"
  mv "$work/android/nsh-embed-android/nsh.aar" "$work/android/nsh-embed-android/nsh-dev.aar" "$home/embed/android/"
  touch "$home/embed/android/.$version"
fi

if [[ "${1:-}" == "--ios" ]]; then
  embed=$(fetch nsh-embed-ios.zip 0a11159c9d368f7558f9b48b62ffcb956a2643ab7ddc21925404ab52dfa563a3)
  unzip -q "$embed" -d "$work/ios"
  rm -rf "$client/ios/Frameworks" && mkdir -p "$client/ios/Frameworks"
  for framework in NativeShell NativeShellDev; do
    found="$(find "$work/ios" -name "$framework.xcframework" -type d | head -1)"
    [[ -n "$found" ]] && mv "$found" "$client/ios/Frameworks/"
  done
  [[ -d "$client/ios/Frameworks/NativeShell.xcframework" ]] || { echo "nsh-embed-ios.zip holds no NativeShell.xcframework" >&2; exit 1; }
fi
"$home/bin/nsh" --version
