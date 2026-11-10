#!/usr/bin/env bash
# Generates client/ios/Mewla.xcodeproj (xcodegen). --dev links NativeShellDev, which can load
# from `nsh dev`: then `xcrun simctl launch booted com.daoleno.mewla -devServer ws://127.0.0.1:8790/ws`.
set -euo pipefail
ios="$(cd "$(dirname "$0")/../ios" && pwd)"
framework=NativeShell
[[ "${1:-}" == "--dev" ]] && framework=NativeShellDev
[[ -d "$ios/Frameworks/$framework.xcframework" ]] || { echo "run client/scripts/fetch-nsh.sh --ios first" >&2; exit 1; }
mkdir -p "$ios/build/nsh"
cd "$ios" && NSH_FRAMEWORK="$framework" xcodegen generate --quiet
