#!/usr/bin/env bash
# The fidelity captures (macos-fidelity.yml): Mewla Dev launched as a person launches it (`open`, so it is the active
# app and its window key), each scenario on a fresh run, the whole main display captured (a HiDPI virtual display at
# 1512 × 982 pt, vdisplay.m) on a neutral desktop with the Dock hidden, plus the on-screen windows' bounds to crop by.
#
#   capture.sh <Mewla Dev.app> <out dir> <light|dark> <name> '<dev script JSON>' [fresh|keep] [menu]
#
# fresh (the default) starts on new state, data and storage (a first run); keep reuses the last run's. `menu` opens
# the menu bar cat's menu (a right-click) before the capture. Writes <out>/<theme>-<name>.png, -windows.txt, -log.txt,
# and for a first run a burst <theme>-<name>-t<seconds>.png from launch. FIDELITY_WINDOW=<w>x<h> opens the main window at
# that size (MEWLA_APP_WINDOW_SIZE); without it the window opens as a person's would (its first size, then the kept one).
set -uo pipefail
app="$1"; out="$2"; theme="$3"; name="$4"; script="$5"; mode="${6:-fresh}"; menu="${7:-}"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$out"
D="${FIDELITY_RUN:-$RUNNER_TEMP/fidelity-run}"
exe="$app/Contents/MacOS/Mewla Dev"
if [[ "$mode" == fresh ]]; then
  rm -rf "$D"
fi
mkdir -p "$D/data"
[[ -f "$D/data/storage.json" ]] || printf '{}' > "$D/data/storage.json"
python3 -I -c 'import json,sys; p=sys.argv[1]; d=json.load(open(p)); d["mewla:theme_preference"]="classic-"+sys.argv[2]; json.dump(d,open(p,"w"))' "$D/data/storage.json" "$theme"
seen=$(cat "$D"/data/logs/* 2>/dev/null | grep -c "dev script: connected" || true)

# As a person opens it: LaunchServices activates it. Its environment names its own state, port and data.
open -n -a "$app" --env MEWLA_APP_STATE_ROOT="$D/state" --env MEWLA_APP_DATA_DIR="$D/data" \
  --env MEWLA_APP_ADDR=127.0.0.1:19877 --env MEWLA_APP_PHONE_ADDRESS=http://127.0.0.1:19877 \
  --env MEWLA_APP_DEV_SCRIPT="$script" ${FIDELITY_WINDOW:+--env MEWLA_APP_WINDOW_SIZE="$FIDELITY_WINDOW"}
t0=$(python3 -c 'import time; print(time.time())')
if [[ "$name" == firstrun ]]; then
  # The first seconds of a first run (the waking state), then ready.
  for t in 1 2 3 4 6 9; do
    while python3 -c "import time,sys; sys.exit(0 if time.time()-$t0 < $t else 1)"; do sleep 0.2; done
    screencapture -x -D 1 "$out/$theme-$name-t$t.png" || true
  done
fi
ok=""
for i in $(seq 1 120); do
  now=$(cat "$D"/data/logs/* 2>/dev/null | grep -c "dev script: connected" || true)
  [[ "${now:-0}" -gt "${seen:-0}" ]] && { ok=1; break; }
  sleep 1
done
pid=$(pgrep -nf "$exe" || true)
sleep "${FIDELITY_SETTLE:-8}"
if [[ "$name" == *terminal* ]]; then
  for i in $(seq 1 40); do cat "$D"/data/logs/* 2>/dev/null | grep -qE "terminal: (opened|error)" && break; sleep 1; done
  sleep 4
fi
if [[ "$name" == *grant* ]]; then
  "$app/Contents/Helpers/mewla" agent-use need chrome --reason "A Worker needs your Chrome to check the release." --state-dir "$D/state" --json >/dev/null 2>&1 || true
  for i in $(seq 1 60); do cat "$D"/data/logs/* 2>/dev/null | grep -qE 'dev script: grant (card|stopped)' && break; sleep 1; done
  sleep 2
fi
if [[ -n "$menu" ]]; then
  # The cat's menu: a right-click on its status item, found through Accessibility (the app's extras menu bar).
  pos=$(osascript -e 'tell application "System Events" to tell (first process whose unix id is '"$pid"') to get {position, size} of menu bar item 1 of menu bar 2' 2>&1 || true)
  echo "status item: $pos" >> "$out/$theme-$name-log.txt"
  read -r x y w h < <(tr -d ',' <<<"$pos")
  if [[ "$x" =~ ^[0-9]+$ ]]; then
    swift "$here/click.swift" right $((x + w / 2)) $((y + h / 2)) || true
    sleep 2
  fi
fi
{
  echo "connected: ${ok:-no}"
  echo "frontmost: $(osascript -e 'tell application "System Events" to get name of first process whose frontmost is true' 2>&1)"
  echo "pid: $pid"
} >> "$out/$theme-$name-log.txt"
swift -e '
import CoreGraphics
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
for w in list {
  let owner = w[kCGWindowOwnerName as String] as? String ?? ""
  let layer = w[kCGWindowLayer as String] as? Int ?? 0
  guard owner == "Mewla Dev" || layer == 25 || layer >= 100 else { continue }
  let b = w[kCGWindowBounds as String] as! [String: Any]
  print(w[kCGWindowNumber as String]!, layer, "\(b["X"]!),\(b["Y"]!) \(b["Width"]!)x\(b["Height"]!)", owner, "|", w[kCGWindowName as String] ?? "")
}' > "$out/$theme-$name-windows.txt" 2>&1 || true
screencapture -x -D 1 "$out/$theme-$name.png" || true
cat "$D"/data/logs/* 2>/dev/null | grep -E "dev script|window:|terminal:|error|Error" | tail -40 >> "$out/$theme-$name-log.txt" || true
head -8 "$out/$theme-$name-log.txt"

# Quit (its daemon, its child, stops with it), then make sure nothing of this run is left.
[[ -n "$pid" ]] && kill -TERM "$pid" 2>/dev/null || true
for i in $(seq 1 60); do pgrep -f "$D" >/dev/null || break; sleep 1; done
pkill -TERM -f "$D" 2>/dev/null || true
sleep 1
