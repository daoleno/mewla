#!/usr/bin/env bash
# Mewla Dev's image input run (macos-image-input.yml): files and images put into its composers as a person puts them
# there, on a CI Mac. Mewla Dev is opened as a person opens it (`open`: it is its own app to macOS, so a folder's
# privacy prompt, if any, names it). Every step is captured and written to <captures>/report.txt; the run fails on any
# step whose composer did not end with the chips it should have. Phases:
#   1. Brain's composer: ⌘V of a screenshot, of an image copied as Preview copies it, of a file copied in Finder, of a
#      spreadsheet cell; a real AppKit drag of a file from another app. Send.
#   2. An image dragged out of a page in Safari and in Chrome, and the screenshot thumbnail dragged in. Send.
#   3. A file in each of Desktop, Documents and Downloads: a real drag out of a Finder window, then a Finder copy and
#      ⌘V, each on macOS's privacy state for those folders reset first. Whether macOS asks, what the prompt says, and
#      what the composer then shows. Documents' drag answers "Don't Allow"; the rest "Allow". Send.
#   4. Sizes: one file of 63 MB, five of 20 MB at once, one of 600 MB (past what a drop reads): time and the app's
#      memory. Send; then the 63 MB file dropped and sent twice more, memory after each.
#   5. A Session's chat (a Session made on this daemon with the stub agent): ⌘V of an image and a drag of a file. Send.
#   6. A Work's reply box (a Work made on this daemon): the same. Send.
#
#   image-input-run.sh <Mewla Dev zip> <mewla CLI>
set +e -u -o pipefail
ZIP="$1"; CLI="$2"
D="$RUNNER_TEMP/image-run"; C="$D/captures"; B="$D/bin"; mkdir -p "$D/data" "$D/site" "$D/project" "$C" "$B"
S="$(cd "$(dirname "$0")" && pwd)"
ID=com.daoleno.mewla.dev
APP="$D/Mewla Dev.app"; EXE="$APP/Contents/MacOS/Mewla Dev"
printf '%s' '{"mewla:theme_preference":"classic-light"}' > "$D/data/storage.json"
ditto -x -k "$ZIP" "$D"
for tool in drag mousedrag ax windows pasteboard; do swiftc -O -o "$B/$tool" "$S/$tool.swift" || exit 1; done

report() { echo "$*" | tee -a "$C/report.txt"; }
fail=0
failed() { report "FAIL: $*"; fail=1; }
log() { cat "$D"/data/logs/* 2>/dev/null; }
wait_log() { for _ in $(seq 1 "${2:-60}"); do log | grep -q "$1" && return 0; sleep 1; done; echo "never logged: $1"; return 1; }
shot() { "$S/capture-windows.sh" "Mewla Dev" "$C/$1" "$1-" >/dev/null || true; screencapture -x "$C/$1/screen.png"; echo "captured $1"; }
front() { osascript -e 'tell application "System Events" to set frontmost of process "Mewla Dev" to true' || echo "could not bring Mewla Dev to the front"; sleep 1; }
key() { osascript -e "tell application \"System Events\" to $1" || echo "could not send: $1"; }
cmd_v() { key 'keystroke "v" using command down'; }
# A launched app can take a while to show its first window (a cold runner's Safari took longer than a fixed wait).
windows_of() { osascript -e "tell application \"System Events\" to count windows of process \"$1\"" 2>/dev/null || echo 0; }
wait_window() { for _ in $(seq 1 "${2:-30}"); do [ "$(windows_of "$1")" -gt 0 ] 2>/dev/null && return 0; sleep 1; done; echo "$1 showed no window in ${2:-30} s"; return 1; }
place() { osascript -e "tell application \"System Events\" to tell process \"$1\"" -e "set position of window 1 to {$2, $3}" -e "set size of window 1 to {$4, $5}" -e 'end tell' || echo "could not place $1"; }
# The newest composer's chips, as the app logs them (mac.ts imageInputLog): "chips N [status name: error]...".
chips() { log | grep "dev script: chips" | tail -1 | sed 's/.*dev script: //'; }
wait_chips() { for _ in $(seq 1 "${2:-60}"); do chips | grep -qE -- "$1" && return 0; sleep 1; done; echo "chips never matched /$1/: $(chips)"; return 1; }
send() { key 'key code 36'; sleep 5; }
now() { python3 -c 'import time; print(f"{time.time():.1f}")'; }
since() { python3 -c "print(f'{$(now) - $1:.1f}')"; }
uploads() { find "$D/state" -path "*uploads*" -type f 2>/dev/null | wc -l | tr -d ' '; }

# Pictures: the screen's top left as PNG (the drag's file, the "Copy Image" source, the page's image).
screencapture -x -R 0,0,480,320 "$D/picture.png"; cp "$D/picture.png" "$D/dragged picture.png"; cp "$D/picture.png" "$D/site/picture.png"
printf '%s' '<!doctype html><html><body style="margin:0"><img src="picture.png" width="400" height="300"></body></html>' > "$D/site/index.html"
(cd "$D/site" && exec python3 -m http.server 8765 --bind 127.0.0.1 > "$D/http.out" 2>&1) &
web=$!

open -n -a "$APP" --stdout "$D/app.out" --stderr "$D/app.out" \
  --env MEWLA_APP_STATE_ROOT="$D/state" --env MEWLA_APP_DATA_DIR="$D/data" --env MEWLA_APP_ADDR=127.0.0.1:19877 \
  --env MEWLA_APP_PHONE_ADDRESS=http://127.0.0.1:19877 --env MEWLA_APP_WINDOW_SIZE=1280x800 \
  --env MEWLA_APP_DEV_SCRIPT='{"imageInput":true,"imageSession":"Drop check","imageWork":"drop-check"}'
wait_log "dev script: image ready" 180 || { log; cat "$D/app.out"; exit 1; }
pid=$(pgrep -f "$EXE" | head -1)
report "Mewla Dev pid $pid, opened with open(1); bundle $ID"
front
shot ready
read -r W H <<<"$(swift -e 'import AppKit; let f = NSScreen.screens[0].frame; print(Int(f.width), Int(f.height))')"
# The main window (the largest of layer 0): a point in its conversation, in global points.
read -r mid_x mid_y <<<"$(python3 -I -c '
import re, sys
best = None
for line in open(sys.argv[1]):
    m = re.match(r"\d+ 0 ([-\d.]+),([-\d.]+) ([\d.]+)x([\d.]+)", line)
    if m:
        x, y, w, h = map(float, m.groups())
        if not best or w * h > best[2] * best[3]: best = (x, y, w, h)
x, y, w, h = best
print(int(x + w * 0.6), int(y + h * 0.45))' "$C/ready/windows.txt")"
report "screen ${W}x${H}, drop point $mid_x,$mid_y"
drop_file() { # <hold s> <marker> <file...>: a real AppKit drag of files from another app onto the drop point
  local hold="$1" marker="$2"; shift 2
  rm -f "$marker"
  "$B/drag" "$mid_x" "$mid_y" "$hold" "$marker" "$@" > "$D/drag.out" 2>&1
  local code=$?
  report "drag of $# file(s): $(grep "drag ended" "$D/drag.out" | tail -1) (exit $code)"
}

report "== 1. Brain's composer: pastes and a drag"
step() { # name, then the command that fills the pasteboard
  local name="$1"; shift
  "$@"
  "$B/pasteboard" types
  front
  cmd_v; sleep 4
  report "$name: $(log | grep "composer: pasted" | tail -1 | sed 's/.*composer: //'); $(chips)"
  shot "$name"
}
step screenshot screencapture -c -x -R 0,0,640,400
step copy-image "$B/pasteboard" image "$D/picture.png"
step finder-file "$B/pasteboard" file "$D/dragged picture.png"
step sheet-cell "$B/pasteboard" rich "Q3 revenue 4,210" "$D/picture.png"
# The drag held for a capture of the drop target ("Drop to attach").
"$B/drag" "$mid_x" "$mid_y" 6 "$D/holding" "$D/dragged picture.png" > "$D/drag.out" 2>&1 &
dragging=$!
for _ in $(seq 1 30); do [ -e "$D/holding" ] && break; sleep 0.5; done
sleep 1.5; shot dragging
wait "$dragging"; report "held drag: $(grep "drag ended" "$D/drag.out")"
front
# Four chips (the screenshot, Copy Image, the Finder file, the dragged file; none for the spreadsheet cell).
wait_chips '^chips 4 ' 30 || failed "phase 1: not four chips"
c=$(chips); report "phase 1 chips: $c"
[ "$(grep -o "\[ready " <<<"$c" | wc -l | tr -d ' ')" = 4 ] || failed "phase 1: not four ready chips"
[ "$(grep -o "\[ready dragged picture.png\]" <<<"$c" | wc -l | tr -d ' ')" = 2 ] || failed "phase 1: the Finder file and the dragged file are not chips under their names"
shot phase1
send; wait_chips '^chips 0$' 20 || failed "phase 1: Send did not take the chips"
[ "$(uploads)" -ge 4 ] || failed "phase 1: the daemon has fewer than four uploads"
shot phase1-sent

report "== 2. An image out of Safari and Chrome, and the screenshot thumbnail"
open -a Safari "http://127.0.0.1:8765/"
# Once more if no window came: Safari can launch without opening the page's window.
wait_window Safari 30 || { open -a Safari "http://127.0.0.1:8765/"; wait_window Safari 30; } || failed "Safari: no window"
sleep 3; place Safari 0 30 440 440; sleep 2
shot safari-open
"$B/mousedrag" 200 250 "$mid_x" "$mid_y" 2 > "$D/mousedrag.out" 2>&1
wait_chips '\[ready picture\.png\]' 30 && report "Safari: $(chips)" || failed "Safari: the page's image is not a chip"
pkill -x Safari; sleep 2
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --user-data-dir="$D/chrome" --no-first-run --no-default-browser-check \
  --window-position=0,30 --window-size=440,440 "http://127.0.0.1:8765/" > "$D/chrome.out" 2>&1 &
wait_window "Google Chrome" 30 || failed "Chrome: no window"
sleep 5; shot chrome-open
"$B/mousedrag" 200 260 "$mid_x" "$mid_y" 2 > "$D/mousedrag.out" 2>&1
wait_chips '\[ready picture\.png\].*\[ready picture\.png\]' 30 && report "Chrome: $(chips)" || failed "Chrome: the page's image is not a chip"
pkill -f "$D/chrome"; sleep 2
front
key 'key code 20 using {command down, shift down}'
sleep 2; screencapture -x "$C/thumbnail-shown.png"
# The floating thumbnail at the screen's bottom right; a still press opens its menu, so the drag moves at once.
"$B/mousedrag" $((W - 67)) $((H - 113)) "$mid_x" "$mid_y" 2 40 > "$D/mousedrag.out" 2>&1
wait_chips '\[ready Screenshot [^]]*\.png\]' 30 && report "thumbnail: $(chips)" || failed "the screenshot thumbnail is not a chip"
front; shot phase2
send; wait_chips '^chips 0$' 20 || failed "phase 2: Send did not take the chips"

report "== 3. Desktop, Documents and Downloads"
defaults write com.apple.finder FXPreferredViewStyle -string Nlsv; killall Finder; sleep 3
report "SIP: $(csrutil status 2>&1)"
report "this user's privacy grants for the three folders and all files: $(sqlite3 "$HOME/Library/Application Support/com.apple.TCC/TCC.db" "select service || ' ' || client || ' ' || auth_value from access where service like 'kTCCServiceSystemPolicy%'" 2>&1 | tr '\n' ';')"
# A window that came since owners-before.txt (taken before the step): logged with what it says; one that asks for a
# folder is a privacy prompt, answered <answer>. Captured as <tag>.png.
answer_prompt() { # <tag> <answer>
  local tag="$1" answer="$2" prompt=""
  screencapture -x "$C/$tag.png"
  "$B/windows" | cut -d'|' -f1 | sort -u > "$D/owners-after.txt"
  for owner in $(comm -13 "$D/owners-before.txt" "$D/owners-after.txt" | grep -vxE "Mewla Dev|Finder|mewla|Screenshot|Privacy Control" | tr ' ' '\037'); do
    owner=$(tr '\037' ' ' <<<"$owner")
    local said; said=$("$B/ax" texts "$owner" 2>/dev/null | tr '\n' ' ')
    report "$tag: a window of '$owner' appeared: $said"
    if grep -qiE "access|folder|allow" <<<"$said"; then prompt="$owner"; fi
  done
  if [ -n "$prompt" ]; then
    "$B/ax" press "$prompt" "$answer" && report "$tag: pressed '$answer' on $prompt's prompt" || failed "$tag: no '$answer' button on $prompt's prompt"
  else
    report "$tag: macOS showed no prompt"
  fi
}
# The control: an app of its own reading a file in Documents by itself. On a Mac that guards the folder, macOS asks.
control="$D/Privacy Control.app"
mkdir -p "$control/Contents/MacOS"
swiftc -O -o "$control/Contents/MacOS/control" "$S/tcc-control.swift"
cat > "$control/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.daoleno.mewla.privacy-control</string>
<key>CFBundleExecutable</key><string>control</string>
<key>CFBundleName</key><string>Privacy Control</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>
PLIST
codesign -s - --force "$control" > /dev/null 2>&1
echo control > "$HOME/Documents/control.txt"
"$B/windows" | cut -d'|' -f1 | sort -u > "$D/owners-before.txt"
open -n -a "$control" --args Documents "$D/control.out"
sleep 4
answer_prompt control-documents Allow
for _ in $(seq 1 20); do [ -s "$D/control.out" ] && break; sleep 1; done
report "control (an app reading ~/Documents by itself): $(cat "$D/control.out" 2>/dev/null || echo "no answer")"
front
folder_case() { # <folder> <drag|paste> <Allow|Don't Allow>
  local folder="$1" how="$2" answer="$3" name="from $1.png"
  local tag; tag="$(tr 'A-Z' 'a-z' <<<"$folder")-$how"
  tccutil reset "SystemPolicy${folder}Folder" "$ID" > /dev/null 2>&1 || report "$tag: tccutil reset failed"
  "$B/windows" | cut -d'|' -f1 | sort -u > "$D/owners-before.txt"
  open "$HOME/$folder"; sleep 3; place Finder 0 30 440 440; sleep 1
  local at=""
  # Finder shows the name without its extension unless it is set to show them all.
  for _ in $(seq 1 10); do at=$("$B/ax" find Finder "$name" || "$B/ax" find Finder "${name%.*}") && break; sleep 1; done
  [ -n "$at" ] || { failed "$tag: no '$name' in the Finder window"; "$B/ax" texts Finder | head -40; return; }
  local t0; t0=$(now)
  if [ "$how" = drag ]; then
    # shellcheck disable=SC2086
    "$B/mousedrag" $at "$mid_x" "$mid_y" 1 300 > "$D/mousedrag.out" 2>&1
  else
    # shellcheck disable=SC2086
    "$B/ax" click Finder $at; sleep 1
    key 'keystroke "c" using command down'; sleep 1
    front; cmd_v
  fi
  sleep 3
  answer_prompt "$tag" "$answer"
  if wait_chips "\[(ready|failed) $name[]:]" 30; then
    report "$tag: after $(since "$t0") s, $(chips | grep -oE "\[[a-z]+ $name[^]]*\]")"
  else
    failed "$tag: '$name' never became a chip ($(chips))"
  fi
  osascript -e 'tell application "System Events" to set frontmost of process "Finder" to true' -e 'tell application "System Events" to keystroke "w" using command down' || true
  front
}
for folder in Desktop Documents Downloads; do
  cp "$D/picture.png" "$HOME/$folder/from $folder.png"
  if [ "$folder" = Documents ]; then folder_case "$folder" drag "Don't Allow"; else folder_case "$folder" drag Allow; fi
  folder_case "$folder" paste Allow
done
# A file this user may not read (mode 000): macOS's pasteboard leaves its URL out for every other app (the item keeps
# only its name as text), so no app hears of it: the drag is refused (no copy cursor) and a paste is its name as text.
cp "$D/picture.png" "$D/locked.png"; chmod 000 "$D/locked.png"
drop_file 1 "$D/m0" "$D/locked.png"
grep -q "drag ended: none" "$D/drag.out" && report "mode-000 file: macOS refused the drag (no file URL reached Mewla Dev)" || failed "a mode-000 file's drag was taken"
"$B/pasteboard" file "$D/locked.png"; front; cmd_v; sleep 3
report "mode-000 file pasted: onPaste reported '$(log | grep "composer: pasted" | tail -1 | sed 's/.*composer: //')'"
key 'keystroke "a" using command down'; key 'key code 51'
# A file that reaches Mewla Dev but cannot be read when it reads it (it went in between): a chip that says so.
unread='\[failed (gone\.png|File): Mewla can.t read this file\.\]'
cp "$D/picture.png" "$D/gone.png"
# Its own marker: the deletion waits until the drag is over the window (a marker left by an earlier drag deleted the
# file before this drag began, and macOS refuses a drag of a file that is not there, as the mode-000 one).
rm -f "$D/m-gone"
"$B/drag" "$mid_x" "$mid_y" 2 "$D/m-gone" "$D/gone.png" > "$D/drag.out" 2>&1 &
dragging=$!
for _ in $(seq 1 30); do [ -e "$D/m-gone" ] && break; sleep 0.3; done
rm -f "$D/gone.png"; wait "$dragging"; report "file gone before the drop: $(grep "drag ended" "$D/drag.out")"
wait_chips "$unread" 30 && report "file gone before the drop: $(chips | grep -oE "\[failed [^]]*\]")" || failed "a dropped file that could not be read has no chip that says why"
unread_before=$(chips | grep -oE "$unread" | wc -l | tr -d ' ')
cp "$D/picture.png" "$D/gone.png"; "$B/pasteboard" file "$D/gone.png"; rm -f "$D/gone.png"; front; cmd_v
wait_chips "($unread.*){$((unread_before + 1))}" 30 && report "file gone before the paste: its own such chip" || failed "a pasted file that could not be read has no chip that says why"
# The same as Finder copies it (a file reference, which names nothing once the file is gone). NSH 0.7 turns Finder's
# references into their paths and leaves out one that names nothing (nsh-platform-macos file_drop.rs local_file_uri):
# the paste is the file's name as text, no file (NSH-ISSUE-gone-finder-reference.md). Until NSH hands it over, that is
# what this checks: no chip, the name in the field (cleared after).
pasted_before=$(log | grep -c "composer: pasted" || true)
cp "$D/picture.png" "$D/copied.png"; "$B/pasteboard" reference "$D/copied.png"; rm -f "$D/copied.png"; front; cmd_v; sleep 3
said=$(log | grep "composer: pasted" | tail -1 | sed 's/.*composer: //')
[ "$(log | grep -c "composer: pasted" || true)" -gt "$pasted_before" ] && grep -q "files 0 uris 0" <<<"$said" && [ "$(chips | grep -oE "$unread" | wc -l | tr -d ' ')" = "$((unread_before + 1))" ] \
  && report "Finder copy gone before the paste: $said, no chip (NSH 0.7 leaves the reference out)" || failed "a Finder copy gone before the paste: not as NSH 0.7 hands it ($said; $(chips))"
key 'keystroke "a" using command down'; key 'key code 51'
report "phase 3 chips: $(chips)"
shot phase3
send; sleep 2; report "phase 3 after Send: $(chips)"
# The failed chips stay after Send; taken off, so the sizes have the composer's eight places to themselves.
for _ in 1 2 3 4; do "$B/ax" press "Mewla Dev" "Remove gone.png" || break; sleep 1; done
wait_chips '^chips 0$' 10 && report "phase 3: the failed chips removed" || failed "phase 3: the failed chips did not come off ($(chips))"

report "== 4. Sizes"
mkfile 63m "$D/clip 63MB.mov"
for i in 1 2 3 4 5; do mkfile 20m "$D/part $i.bin"; done
mkfile -n 600m "$D/huge 600MB.mov"
(while kill -0 "$pid" 2>/dev/null; do ps -o rss= -p "$pid"; sleep 0.5; done > "$D/rss.txt") &
sampler=$!
rss_now() { ps -o rss= -p "$pid" | tr -d ' '; }
report "app memory before: $(rss_now) KB"
t0=$(now); drop_file 1 "$D/m1" "$D/clip 63MB.mov"
wait_chips '\[ready clip 63MB\.mov\]' 180 && report "63 MB file: ready after $(since "$t0") s; memory $(rss_now) KB" || failed "the 63 MB file is not a ready chip"
t0=$(now); drop_file 1 "$D/m2" "$D/part 1.bin" "$D/part 2.bin" "$D/part 3.bin" "$D/part 4.bin" "$D/part 5.bin"
wait_chips '(\[ready part [1-5]\.bin\].*){5}' 180 && report "five 20 MB files at once: ready after $(since "$t0") s; memory $(rss_now) KB" || failed "the five 20 MB files are not ready chips"
t0=$(now); drop_file 1 "$D/m3" "$D/huge 600MB.mov"
# A local file is read by the app up to 64 MB (NSH 0.7 hands a drop's local file over as its URL); with its contents
# from NSH (longbridge/nsh#3015, #3016) the limit is a drop's 512 MB.
wait_chips '\[failed huge 600MB\.mov: This file is larger than (64|512) MB\.\]' 60 && report "600 MB file: $(since "$t0") s to its failed chip" || failed "the 600 MB file has no failed chip that says why"
front; shot phase4
kill "$sampler" 2>/dev/null
report "app memory peak during sizes: $(sort -n "$D/rss.txt" | tail -1 | tr -d ' ') KB"
send; sleep 3; report "phase 4 after Send: $(chips)"
sleep 15; report "app memory 15 s after Send: $(rss_now) KB"
# Whether memory comes back: the same 63 MB file dropped and sent twice more. Flat is what the allocator keeps; a
# climb of about the file's size each time is a drop kept alive.
for round in 2 3; do
  drop_file 1 "$D/m4-$round" "$D/clip 63MB.mov"
  wait_chips '\[ready clip 63MB\.mov\]' 180 || failed "the 63 MB file is not a ready chip again (round $round)"
  send; wait_chips '^chips [0-9]+( \[failed [^]]*\])*$' 30 || failed "Send did not take the 63 MB file (round $round)"
  sleep 15; report "app memory 15 s after the 63 MB file's Send, round $round: $(rss_now) KB"
done

report "== 5. A Session's chat"
MEWLA_STATE_ROOT="$D/state" "$CLI" worker spawn -name "Drop check" -executor claude -cwd "$D/project" -prompt "Wait for files." > "$D/spawn.out" 2>&1 \
  || { failed "could not make a Session"; cat "$D/spawn.out"; }
if wait_log "dev script: session ready" 90; then
  front
  "$B/pasteboard" image "$D/picture.png"; cmd_v; sleep 2
  drop_file 1 "$D/m4" "$D/dragged picture.png"
  wait_chips '^chips 2 .*\[ready Pasted image [^]]*\].*\[ready dragged picture\.png\]|^chips 2 .*\[ready dragged picture\.png\].*\[ready Pasted image' 60 \
    && report "Session: $(chips)" || failed "Session: not the pasted image and the dropped file as chips ($(chips))"
  shot session
  send; wait_chips '^chips 0$' 20 && report "Session: Send took the chips" || failed "Session: Send did not take the chips"
  shot session-sent
else
  failed "the Session did not open"; cat "$D/spawn.out"
fi

report "== 6. A Work's reply box"
MEWLA_STATE_ROOT="$D/state" "$CLI" brain work create -id drop-check -title "Drop check" -objective "Files into a Work's reply box" > "$D/work.out" 2>&1 \
  || { failed "could not make a Work"; cat "$D/work.out"; }
if wait_log "dev script: work ready" 90; then
  front
  "$B/pasteboard" image "$D/picture.png"; cmd_v; sleep 2
  drop_file 1 "$D/m5" "$D/dragged picture.png"
  wait_chips '^chips 2 .*\[ready Pasted image [^]]*\].*\[ready dragged picture\.png\]|^chips 2 .*\[ready dragged picture\.png\].*\[ready Pasted image' 60 \
    && report "Work: $(chips)" || failed "Work: not the pasted image and the dropped file as chips ($(chips))"
  shot work
  send; wait_chips '^chips 0$' 20 && report "Work: Send took the chips" || failed "Work: Send did not take the chips"
  shot work-sent
else
  failed "the Work did not open"; cat "$D/work.out"
fi

log | grep -E "dev script|composer:" > "$C/app-log.txt"
report "files the daemon took: $(uploads)"
kill -TERM "$pid" 2>/dev/null; kill "$web" 2>/dev/null
for _ in $(seq 1 60); do pgrep -f "$EXE" >/dev/null || break; sleep 1; done
pkill -TERM -f "$EXE" || true
if log | grep -E "ERROR (js|nsh)"; then failed "the app logged errors"; fi
exit $fail
