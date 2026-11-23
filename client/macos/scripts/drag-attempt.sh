#!/usr/bin/env bash
# One bounded attempt at the drag itself on a CI Mac (macos-permissions.yml `drag`): with no row for Mewla, Allow
# opens System Settings on the list and the panel beside it; synthetic pointer events (CGEvent, posted from this job's
# pre-granted shell) press the panel's icon, move into the list and let go, holding over the list for a capture. System
# Settings takes the drop and asks for an administrator's password ("Privacy & Security is trying to modify your
# system settings"), where a person types theirs; the runner cannot. Each step's screen is kept, and whether macOS
# wrote a row.
#   client/macos/scripts/drag-attempt.sh <Mewla.app> <out dir>
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo "this script edits the system TCC.db: a CI Mac only" >&2; exit 2; }
src="${1:?Mewla.app}"
out="${2:?out dir}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
db="/Library/Application Support/com.apple.TCC/TCC.db"
app=/Applications/Mewla.app
cli="$app/Contents/Helpers/mewla"
data="$work/data"
mkdir -p "$data"
log="$out/drag.txt"
: > "$log"
say() { echo "== $*" | tee -a "$log"; }
tcc_reload() { sudo killall tccd 2>/dev/null || true; killall tccd 2>/dev/null || true; sleep 2; }
rows() { sudo sqlite3 "$db" "select service, client, client_type, auth_value from access where client = 'com.daoleno.mewla' or client like '$app/%';"; }
shot() { screencapture -x "$out/$1.png" || true; }
app_log() { cat "$data"/logs/* 2>/dev/null || true; }

sudo rm -rf "$app"
ditto "$src" "$app"
sudo sqlite3 "$db" "delete from access where client = 'com.daoleno.mewla' or client like '$app/%';"
tcc_reload
# The prompt asks for an administrator's password, which a runner's account does not give out (not one of the image's
# known ones; root cannot set a new one without the old: tried in run 45132805657).

cat > "$work/drag.swift" <<'SWIFT'
import CoreGraphics
import Foundation

// drag list | drag <service label> : press the Mewla panel's icon, move into System Settings' list, hold, let go.
func windows() -> [[String: Any]] {
  (CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]]) ?? []
}
func bounds(_ w: [String: Any]) -> CGRect {
  CGRect(dictionaryRepresentation: (w[kCGWindowBounds as String] as! CFDictionary)) ?? .zero
}
let all = windows()
if CommandLine.arguments.count > 1 && CommandLine.arguments[1] == "list" {
  for w in all {
    let owner = w[kCGWindowOwnerName as String] as? String ?? "?"
    let layer = w[kCGWindowLayer as String] as? Int ?? -1
    print("\(owner) layer=\(layer) \(bounds(w))")
  }
  exit(0)
}
guard let panel = all.first(where: { ($0[kCGWindowOwnerName as String] as? String) == "Mewla" && abs(bounds($0).width - 248) < 2 }) else {
  print("no Mewla panel on screen"); exit(3)
}
guard let settings = all.filter({ ($0[kCGWindowOwnerName as String] as? String) == "System Settings" && ($0[kCGWindowLayer as String] as? Int) == 0 })
  .max(by: { bounds($0).width * bounds($0).height < bounds($1).width * bounds($1).height }) else {
  print("no System Settings window on screen"); exit(4)
}
let p = bounds(panel), s = bounds(settings)
let from = CGPoint(x: p.minX + 124, y: p.minY + 70)
// The list: the window's right part, on its first rows (both panes' lists start there).
let to = CGPoint(x: s.minX + s.width * 0.65, y: s.minY + 190)
print("panel \(p) settings \(s) from \(from) to \(to)")
let source = CGEventSource(stateID: .hidSystemState)
func post(_ type: CGEventType, _ at: CGPoint) {
  CGEvent(mouseEventSource: source, mouseType: type, mouseCursorPosition: at, mouseButton: .left)?.post(tap: .cghidEventTap)
}
post(.mouseMoved, from); usleep(300_000)
post(.leftMouseDown, from); usleep(250_000)
// Slowly, as a hand does (the drag image follows the pointer behind it).
let steps = 120
for i in 1...steps {
  let t = CGFloat(i) / CGFloat(steps)
  post(.leftMouseDragged, CGPoint(x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t))
  usleep(40_000)
}
// Hold over the list (the run captures the screen now), with small moves so the drop target keeps tracking.
for i in 0..<50 {
  post(.leftMouseDragged, CGPoint(x: to.x + CGFloat(i % 3) * 2, y: to.y + CGFloat(i % 2) * 2))
  usleep(100_000)
}
post(.leftMouseUp, to)
print("dropped at \(to)")
SWIFT
swiftc -O "$work/drag.swift" -o "$work/drag" 2>&1 | tail -5


for permission in accessibility screen_recording; do
  service=kTCCServiceAccessibility; [[ $permission == screen_recording ]] && service=kTCCServiceScreenCapture
  say "$permission: Allow, then the drag"
  osascript -e 'tell application id "com.daoleno.mewla" to quit' >/dev/null 2>&1 || true
  osascript -e 'tell application id "com.apple.systempreferences" to quit' >/dev/null 2>&1 || true
  sleep 2
  before=$(app_log | grep -c "dev script: allow $permission" || true)
  open --env "MEWLA_APP_DATA_DIR=$data" --env "MEWLA_APP_DEV_SCRIPT={\"settings\":\"access\",\"allow\":\"$permission\"}" "$app"
  for _ in $(seq 1 120); do [[ $(app_log | grep -c "dev script: allow $permission" || true) -gt $before ]] && break; sleep 1; done
  sleep 6
  shot "$permission-0-panel"
  "$work/drag" list | tee -a "$log"
  "$work/drag" "$permission" > "$work/drag.out" 2>&1 &
  dragger=$!
  sleep 6.5; screencapture -x -C "$out/$permission-1-holding-over-list.png" || true
  sleep 2; screencapture -x -C "$out/$permission-1b-holding-later.png" || true
  wait "$dragger" || true
  tee -a "$log" < "$work/drag.out"
  sleep 3; shot "$permission-2-after-drop"
  osascript -e 'tell application "System Events" to get name of every process whose name contains "SecurityAgent"' 2>&1 | sed 's/^/password prompt process: /' | tee -a "$log" || true
  sleep 2; shot "$permission-3-password-prompt"
  say "$permission rows: $(rows | tr '\n' ' ')"
  say "$permission daemon: $("$cli" agent-use status --json 2>/dev/null | python3 -I -c 'import json,sys; print(" ".join(g["id"]+"="+g["state"] for g in (json.load(sys.stdin).get("computer") or {}).get("permissions") or []))' || true)"
  app_log | grep -E "dev script: (grants|allow)" | tail -6 | tee -a "$log"
done
log show --last 20m --style compact --predicate 'process == "tccd"' 2>/dev/null | grep -i -E "mewla|insert|auth" | tail -200 > "$out/tccd.log" || true
