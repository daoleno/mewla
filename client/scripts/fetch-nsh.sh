#!/usr/bin/env bash
# Installs the pinned NSH into $NSH_HOME (default ~/.nsh): the NSH 0.6.1 release's CLI (it carries this
# machine's runtime and the web runtime, and builds the JS bundle) and the Android AARs the app embeds
# ($NSH_HOME/embed/android); with --ios, NativeShell(.Dev).xcframework into client/ios/Frameworks.
# Checksums are pinned here: nothing is installed that was not checked (NSH 0.5 also adds other platforms'
# runtimes on demand; the app's builds use none of them).
#
# The embedded runtimes come from one commit of daoleno/nsh's line mewla/v0.6.1-file-contents (NSH 0.6.1 plus
# what Mewla needs before NSH ships it: file drops and pastes with their contents, a closed sheet's late dismissal
# kept off the page under it, Android text selection), the same commit client/macos/Cargo.toml pins for the Mac app
# (scripts/tests/test_nsh_pin.py holds them together). That line's CI (.github/workflows/mewla-embed.yml)
# builds the two embedding archives from the commit once and publishes them as its prerelease mewla-<commit>.
# Goes once an NSH release has longbridge/nsh#2964 (merged), #3014, #3015, #3016, #3017 and #3018-#3023.
set -euo pipefail
version="v0.6.1"
fork="77f4496c8d1daa38e417c9ac2cf83c1be1bdb199"
base="https://github.com/longbridge/nsh/releases/download/$version"
embed_base="https://github.com/daoleno/nsh/releases/download/mewla-$fork"
home="${NSH_HOME:-$HOME/.nsh}"
client="$(cd "$(dirname "$0")/.." && pwd)"

fetch() { # asset sha256 [base] -> path of the verified download
  local file="$work/$1"
  curl -fsSL --retry 3 -o "$file" "${3:-$base}/$1"
  [[ "$(shasum -a 256 "$file" | cut -d' ' -f1)" == "$2" ]] || { echo "checksum of $1 does not match" >&2; exit 1; }
  echo "$file"
}
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [[ "$("$home/bin/nsh" --version 2>/dev/null | cut -d' ' -f2)" != "${version#v}" ]]; then
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) cli=$(fetch nsh-linux-x86_64.tar.gz 9a7ac692f1c229df476be8ff83a3c5912d271453b644e313f02cb19d18696070) ;;
    Darwin-arm64) cli=$(fetch nsh-macos-arm64.tar.gz ee1aed2a4d111fe90e6cceca4b9d5f834c9e11cfda525f8be984261a329c06de) ;;
    *) echo "no NSH $version CLI for $(uname -sm)" >&2; exit 1 ;;
  esac
  # Never replaces someone's own NSH: an existing $NSH_HOME of another version is an error.
  [[ ! -e "$home" ]] || { echo "$home holds another NSH; set NSH_HOME to a new directory" >&2; exit 1; }
  tar -xzf "$cli" -C "$work"
  mkdir -p "$(dirname "$home")" && mv "$work/nsh" "$home"
fi

# The Android AARs (Gradle reads $NSH_HOME/embed/android/nsh(-dev).aar); since 0.5.0 the CLI archive no longer carries them.
if [[ ! -f "$home/embed/android/.$fork" ]]; then
  embed=$(fetch nsh-embed-android.zip 148979b8977a67bfc855073665015806a507ef72132561fbedb33b7d9223b895 "$embed_base")
  unzip -q "$embed" -d "$work/android"
  rm -rf "$home/embed/android" && mkdir -p "$home/embed/android"
  mv "$work/android/nsh-embed-android/nsh.aar" "$work/android/nsh-embed-android/nsh-dev.aar" "$home/embed/android/"
  touch "$home/embed/android/.$fork"
fi

if [[ "${1:-}" == "--ios" ]]; then
  embed=$(fetch nsh-embed-ios.zip 4a1114644dbf1c159085139bf59971d49578cfbb3f903d106c15537e68711b9e "$embed_base")
  unzip -q "$embed" -d "$work/ios"
  rm -rf "$client/ios/Frameworks" && mkdir -p "$client/ios/Frameworks"
  for framework in NativeShell NativeShellDev; do
    found="$(find "$work/ios" -name "$framework.xcframework" -type d | head -1)"
    [[ -n "$found" ]] && mv "$found" "$client/ios/Frameworks/"
  done
  [[ -d "$client/ios/Frameworks/NativeShell.xcframework" ]] || { echo "nsh-embed-ios.zip holds no NativeShell.xcframework" >&2; exit 1; }
fi
"$home/bin/nsh" --version
