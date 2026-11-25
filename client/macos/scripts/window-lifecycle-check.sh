#!/usr/bin/env bash
# Mewla for Mac's window as a person meets it, on NSH 0.7's windows (macos-app.yml: a runner's screen, real input):
#   1. the main window's close button hides it: the app (the menu bar cat) and its daemon run on, and the cat offers the
#      quick chat's shortcut;
#   2. the Dock's reopen (`open` of the running app) shows the same window again;
#   3. a click in Brain's composer gives it the keys, and what is typed lands in it (NSH 0.7: a press no longer moves
#      the keyboard focus to a control); closed and reopened from the Dock, that draft is still there;
#   4. the quick chat's shortcut (⇧⌘Space) opens the perch;
#   5. Quit (as the app menu's) ends the app and its daemon (`mewla serve`). Its session host and Sessions run on, as on
#      any daemon stop (the daemon takes them down only when the app is gone without a Quit); this run stops them after.
#   6. Quit in full screen and opened again: on screen, in full screen.
# Usage: window-lifecycle-check.sh <dir holding Mewla Dev.app> <captures dir>. Its own state, data and port.
set -uo pipefail
D=$1
C=$2
S=$(cd "$(dirname "$0")" && pwd)
W="$D/window-lifecycle"
mkdir -p "$C" "$W/data"
swiftc -O -o "$W/ax" "$S/ax.swift" || exit 1
APP="Mewla Dev"
printf '%s' '{"mewla:theme_preference":"classic-light"}' > "$W/data/storage.json"

log() { cat "$W"/data/logs/* 2>/dev/null; }
count() { log | grep -c -- "$1" || true; }
failed=""
fail() { echo "FAIL: $*"; failed=1; }
# Waits until the log has more than `seen` lines matching `pattern`.
wait_more() { local pattern=$1 seen=$2 secs=${3:-30}; for _ in $(seq 1 "$secs"); do [ "$(count "$pattern")" -gt "$seen" ] && return 0; sleep 1; done; return 1; }
# The daemon itself (`mewla serve` on this run's port), not the session host that outlives it by design.
daemon_up() { pgrep -f "serve -addr 127.0.0.1:19878" >/dev/null; }
shot() { "$S/capture-windows.sh" "$APP" "$C" "$1-" >/dev/null 2>&1 || true; }

# Mewla Dev on this run's state, with its dev script $1; its pid in $pid.
start_app() {
  env -i HOME="$HOME" USER="$USER" LOGNAME="$USER" SHELL=/bin/bash LANG=en_US.UTF-8 TMPDIR="$TMPDIR" PATH=/usr/bin:/bin:/usr/sbin:/sbin \
    MEWLA_APP_STATE_ROOT="$W/state" MEWLA_APP_DATA_DIR="$W/data" MEWLA_APP_ADDR=127.0.0.1:19878 \
    MEWLA_APP_PHONE_ADDRESS=http://127.0.0.1:19878 MEWLA_APP_DEV_SCRIPT="$1" \
    "$D/$APP.app/Contents/MacOS/$APP" >> "$W/app.out" 2>&1 &
  pid=$!
}
# The app's windows on screen (Core Graphics, whatever its Space): "<width>x<height>" each.
on_screen() { swift -e 'import CoreGraphics; import Foundation; let l = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]; for w in l where (w[kCGWindowOwnerName as String] as? String) == CommandLine.arguments[1] && (w[kCGWindowLayer as String] as? Int) == 0 { let b = w[kCGWindowBounds as String] as! [String: Any]; print("\((b["Width"] as! NSNumber).intValue)x\((b["Height"] as! NSNumber).intValue)") }' "$APP"; }
start_app '{}'
wait_more "dev script: connected" 0 120 || { echo "Mewla Dev did not join its daemon"; log | tail -40; kill "$pid"; exit 1; }
wait_more "window: main shown" 0 30 || fail "the window never showed"
sleep 3
shot open

# 1. The close button: hidden, not closed; the app and its daemon run on; the cat offers the shortcut.
"$W/ax" windows "$APP"
"$W/ax" close "$APP" || fail "no close button to press"
wait_more "window: main hidden" 0 15 && echo "1. closed: the window hid" || fail "the window did not hide on its close button"
sleep 3
kill -0 "$pid" 2>/dev/null && echo "1. the app runs on" || fail "the app ended with its window"
daemon_up && echo "1. its daemon runs on" || fail "the daemon stopped with the window"
"$W/ax" windows "$APP" | tee "$W/closed.txt"
grep -q "^280x134" "$W/closed.txt" && echo "1. the shortcut offer is up" || fail "no shortcut offer after the first close"
shot closed

# 2. The Dock's reopen: the same window, shown again.
shown=$(count "window: main shown")
open -a "$D/$APP.app"
wait_more "window: main shown" "$shown" 15 && echo "2. reopened: the window shows again" || fail "the Dock's reopen did not show the window"
sleep 3
shot reopened

# 3. A click in the composer, then typing.
at=$("$W/ax" field "$APP") || fail "no composer to click"
if [ -n "${at:-}" ]; then
  "$W/ax" click "$APP" ${at}
  sleep 1
  osascript -e 'tell application "System Events" to keystroke "typed on the Mac"'
  sleep 2
  # macOS may capitalize the first letter (its text substitutions): matched in any case.
  "$W/ax" texts "$APP" | grep -qi "typed on the Mac" && echo "3. the click gave the composer the keys: the typing is in it" || fail "the typing did not land in the composer"
  shot typed
  # Closed and brought back from the Dock again: the draft is still in the composer.
  hidden=$(count "window: main hidden")
  "$W/ax" close "$APP" || fail "no close button to press the second time"
  wait_more "window: main hidden" "$hidden" 15 || fail "the window did not hide on its second close"
  sleep 2
  shown=$(count "window: main shown")
  open -a "$D/$APP.app"
  wait_more "window: main shown" "$shown" 15 || fail "the Dock's second reopen did not show the window"
  sleep 3
  "$W/ax" texts "$APP" | grep -qi "typed on the Mac" && echo "3. closed and reopened: the draft is still in the composer" || fail "the draft was gone after a close and the Dock's reopen"
  shot draft-kept
fi

# 4. The quick chat's shortcut.
before=$("$W/ax" windows "$APP" | wc -l)
osascript -e 'tell application "System Events" to key code 49 using {command down, shift down}'
sleep 3
after=$("$W/ax" windows "$APP" | wc -l)
"$W/ax" windows "$APP"
[ "$after" -gt "$before" ] && echo "4. ⇧⌘Space opened the perch" || fail "⇧⌘Space opened no perch ($before windows, then $after)"
shot perch
osascript -e 'tell application "System Events" to key code 53'
sleep 1

# 5. Quit: the app ends, and Mewla Dev's daemon (its child) with it.
osascript -e 'tell application id "com.daoleno.mewla.dev" to quit' || true
for i in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
kill -0 "$pid" 2>/dev/null && fail "Quit did not end the app" || echo "5. Quit ended the app"
for i in $(seq 1 120); do daemon_up || break; sleep 1; done
daemon_up && fail "the daemon outlived Quit" || echo "5. its daemon stopped with it"
# Its session host and Sessions, then anything else of this run's state.
pkill -TERM -f "$W/state" || true

# 6. Quit in full screen, opened again: the window is on screen, in full screen. NSH 0.7 entered a kept full screen into a
#    Space macOS never brought up, and the app ran with no window (ui/mac/mac.ts reenterFullScreen).
connected=$(count "dev script: connected")
start_app '{"fullScreen":true}'
wait_more "dev script: connected" "$connected" 120 || fail "Mewla Dev did not join its daemon again"
for _ in $(seq 1 20); do on_screen | grep -q "^1024x768" && break; sleep 1; done
on_screen | grep -q "^1024x768" && echo "6. in full screen" || fail "the dev script's full screen is not on screen"
kill -TERM "$pid"; for _ in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
for _ in $(seq 1 30); do pgrep -f "$W/state" >/dev/null || break; sleep 1; done
connected=$(count "dev script: connected"); back=$(count "window: main back in full screen")
start_app '{}'
wait_more "dev script: connected" "$connected" 120 || fail "Mewla Dev did not join its daemon after the full screen run"
wait_more "window: main back in full screen" "$back" 20 || fail "the kept full screen was not entered again"
for _ in $(seq 1 20); do on_screen | grep -q "^1024x768" && break; sleep 1; done
on_screen | grep -q "^1024x768" && echo "6. opened again: on screen, in full screen" || fail "opened again after a full screen Quit: no window on screen ($(on_screen | tr '\n' ' '))"
shot fullscreen-again
kill -TERM "$pid"; for _ in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
pkill -TERM -f "$W/state" || true

log | grep -E "window: main|dock: shown" || true
[ -z "$failed" ]
