#!/usr/bin/env bash
# Captures the on-screen windows of a running app by window id, without activating it:
#   client/macos/scripts/capture-windows.sh "Mewla Dev" <out dir> [prefix]
# Writes <prefix><window number>.png and windows.txt (number, layer, bounds, name). Needs Screen Recording for the
# shell that runs it; prints "no screen capture access" and exits 2 without it (it never prompts).
set -euo pipefail

owner="$1"
out="$2"
prefix="${3:-win-}"
mkdir -p "$out"

if [[ "$(swift -e 'import CoreGraphics; print(CGPreflightScreenCaptureAccess())' 2>/dev/null)" != "true" ]]; then
  echo "no screen capture access"
  exit 2
fi

swift -e '
import CoreGraphics
let owner = CommandLine.arguments[1]
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
// The app windows. Its menu bar item is not among them: from macOS 26 Control Center hosts every status item.
for w in list where (w[kCGWindowOwnerName as String] as? String) == owner {
  let b = w[kCGWindowBounds as String] as! [String: Any]
  print(w[kCGWindowNumber as String]!, w[kCGWindowLayer as String]!, "\(b["X"]!),\(b["Y"]!) \(b["Width"]!)x\(b["Height"]!)", w[kCGWindowName as String] ?? "")
}' "$owner" > "$out/windows.txt"

cat "$out/windows.txt"
while read -r number _; do
  screencapture -x -o -l "$number" "$out/$prefix$number.png"
done < "$out/windows.txt"
