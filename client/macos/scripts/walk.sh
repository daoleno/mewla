#!/usr/bin/env bash
# A stranger's walk with Mewla for Mac on a clean Mac (macos-release.yml, `walk`): the .dmg the site links to, downloaded
# as a browser does (the quarantine stays), Gatekeeper on the image, the image opened, the app dragged to Applications
# (a Finder copy), the first launch through LaunchServices with Gatekeeper's question answered Open, the first window,
# Pair a phone… from the menu bar cat, Quit and a second launch; then the shipped app's own surfaces (first run,
# Settings, Calendar) in light and dark for the look check. A capture of each step; a check that fails is named in
# <out>/walk.txt and makes the script exit 1 at the end (the captures are taken whatever happens).
#
#   client/macos/scripts/walk.sh <dmg URL | dmg file> <out dir>
#
# Needs a runner's Mac: Screen Recording and Accessibility for the shell (as GitHub's macOS runners have), a HiDPI
# virtual display (fidelity/vdisplay.m) for captures at 2 px per pt. An agent CLI on PATH makes Brain start (a runner
# has none: macos-release.yml installs the stand-in stub-claude.c, as a person with Claude Code installed has).
set -uo pipefail
src="$1"; out="$2"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$out"
log="$out/walk.txt"
: > "$log"
failed=()
say() { echo "$*" | tee -a "$log"; }
# <seconds> <command...>: the command, killed if it is still running then (an AppleScript waiting on a prompt, a
# detach the Finder holds up). macOS has no timeout(1).
bounded() {
  local secs="$1"; shift
  "$@" &
  local pid=$!
  # Its output is not the command's: inside $(…) the guard would hold the pipe open until its sleep ends.
  ( sleep "$secs"; kill -9 "$pid" 2>/dev/null ) > /dev/null 2>&1 < /dev/null &
  local guard=$!
  wait "$pid"; local rc=$?
  kill "$guard" 2>/dev/null; wait "$guard" 2>/dev/null
  return $rc
}
osa() { bounded 20 osascript "$@"; }
# Opening the app as a double-click does, in the background: `open` waits while Gatekeeper's question is up.
launch() { open "$@" > /dev/null 2>&1 & }
check() { local what="$1"; shift; if "$@" >> "$log" 2>&1; then say "ok: $what"; else say "FAILED: $what"; failed+=("$what"); fi; }
shot() { bounded 20 screencapture -x -D 1 "$out/$1.png" 2>/dev/null || bounded 20 screencapture -x "$out/$1.png" || true; say "capture: $1.png"; }
# The app's on-screen windows (layer 0), each alone: <name>-w<number>.png, and their bounds in <name>-windows.txt.
windows() {
  bounded 90 swift -e '
import CoreGraphics
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
for w in list where (w[kCGWindowOwnerName as String] as? String) == "Mewla" && (w[kCGWindowLayer as String] as? Int) == 0 {
  let b = w[kCGWindowBounds as String] as! [String: Any]
  print(w[kCGWindowNumber as String]!, "\(b["X"]!),\(b["Y"]!) \(b["Width"]!)x\(b["Height"]!)", w[kCGWindowName as String] ?? "")
}' > "$out/$1-windows.txt" 2>&1 || true
  while read -r number _; do
    [[ "$number" =~ ^[0-9]+$ ]] && bounded 20 screencapture -x -o -l "$number" "$out/$1-w$number.png" || true
  done < "$out/$1-windows.txt"
  say "windows ($1): $(tr '\n' ';' < "$out/$1-windows.txt")"
}
appearance() { osa -e "tell application \"System Events\" to tell appearance preferences to set dark mode to $1" >/dev/null 2>&1 || true; sleep 3; }
both() { appearance false; shot "$1-light"; windows "$1-light"; appearance true; shot "$1-dark"; windows "$1-dark"; appearance false; }
app_pid() { pgrep -f '/Applications/Mewla.app/Contents/MacOS/Mewla' | head -1; }
app_up() { local p; p=$(app_pid); [[ -n "$p" ]] && [[ "$(ps -o state= -p "$p" | tr -d ' ')" != T* ]]; }
has_window() { bounded 60 swift -e 'import CoreGraphics
let l = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
print(l.contains { ($0[kCGWindowOwnerName as String] as? String) == "Mewla" && ($0[kCGWindowLayer as String] as? Int) == 0 } ? "yes" : "no")' 2>/dev/null | grep -q yes; }
up_with_window() { app_up && has_window; }
health() { curl -m 5 -fsS http://127.0.0.1:9876/health 2>/dev/null; }
quit_app() {
  osa -e 'quit app "Mewla"' >/dev/null 2>&1 || true
  for i in $(seq 1 30); do [[ -z "$(app_pid)" ]] && return 0; sleep 1; done
  return 1
}
# Gatekeeper's question, wherever it is shown: the window with an Open button. Captured, then answered Open.
# (A file, not a heredoc: bounded runs it in the background, where bash gives it /dev/null for input.)
gatekeeper_scpt="$out/.gatekeeper.applescript"
cat > "$gatekeeper_scpt" <<'OSA'
tell application "System Events"
  set ps to {}
  try
    set ps to ps & (every process whose name is "CoreServicesUIAgent")
  end try
  set ps to ps & (every process)
  repeat with p in ps
    try
      if (count of windows of p) > 0 then
        set b to {}
        try
          set b to name of every button of window 1 of p
        end try
        if b contains "Open" then
          set t to ""
          try
            set t to (value of every static text of window 1 of p) as string
          end try
          return (name of p) & " :: " & t
        end if
      end if
    end try
  end repeat
end tell
return ""
OSA
answer_gatekeeper() { bounded 20 osascript "$gatekeeper_scpt" 2>&1; }
press_open() {
  osa -e 'tell application "System Events" to tell process "CoreServicesUIAgent" to click button "Open" of window 1' >/dev/null 2>&1 && { echo CoreServicesUIAgent; return 0; }
  osa -e 'tell application "System Events" to repeat with p in (every process)
try
if (count of windows of p) > 0 then
if (name of every button of window 1 of p) contains "Open" then
click button "Open" of window 1 of p
return name of p
end if
end if
end try
end repeat' 2>&1
}

appearance false

# 1. The download: the site's link, followed as a browser follows it, and the quarantine a browser puts on it.
dmg="$HOME/Downloads/Mewla-macos-arm64.dmg"
mkdir -p "$HOME/Downloads"
if [[ "$src" == http* ]]; then
  node "$here/landing-download.mjs" "$src" "$dmg" "$out" >> "$log" 2>&1 || exit 1
  actual=$(shasum -a 256 "$dmg" | cut -d' ' -f1)
  [[ "sha256:$actual" == "$PUBLIC_DMG_DIGEST" ]] || { say "FAILED: download digest differs from the public asset"; exit 1; }
else
  cp "$src" "$dmg"
fi
check "the download is a disk image" test -s "$dmg"
if xattr -p com.apple.quarantine "$dmg" >/dev/null 2>&1; then
  say "preserved the download's quarantine"
else
  xattr -w com.apple.quarantine "0083;$(printf '%x' "$(date +%s)");MewlaDownloadWalk;$(uuidgen)" "$dmg" || exit 1
  say "runner applied quarantine before opening the download"
fi
xattr -w com.apple.metadata:kMDItemWhereFroms "$src" "$dmg" 2>/dev/null || true
say "sha256: $(shasum -a 256 "$dmg" | cut -d' ' -f1)"
check "Gatekeeper accepts the image (spctl)" spctl -a -t open --context context:primary-signature -vv "$dmg"
check "the image's notarization ticket is stapled" xcrun stapler validate "$dmg"

# 2. Double-click the image: LaunchServices mounts it and the Finder shows the app beside Applications.
bounded 60 open "$dmg"
for i in $(seq 1 60); do [[ -d /Volumes/Mewla/Mewla.app ]] && break; sleep 1; done
check "the image opens with the app and Applications" test -d /Volumes/Mewla/Mewla.app -a -L /Volumes/Mewla/Applications
sleep 4
shot 01-image-open
check "Gatekeeper accepts the app in the image" spctl -a -t exec -vv /Volumes/Mewla/Mewla.app

# 3. Drag Mewla onto Applications: the Finder's copy (it keeps the image's quarantine).
if ! osa -e 'tell application "Finder" to duplicate (POSIX file "/Volumes/Mewla/Mewla.app" as alias) to (path to applications folder)' >> "$log" 2>&1; then
  say "the Finder's copy was not allowed to this shell; copied as the Finder copies (ditto, quarantine kept)"
  ditto /Volumes/Mewla/Mewla.app /Applications/Mewla.app
fi
check "Mewla is in Applications" test -x /Applications/Mewla.app/Contents/MacOS/Mewla
xattr -p com.apple.quarantine /Applications/Mewla.app >/dev/null 2>&1 || xattr -w com.apple.quarantine "0083;$(printf '%x' "$(date +%s)");Safari;$(uuidgen)" /Applications/Mewla.app
say "quarantine on the installed app: $(xattr -p com.apple.quarantine /Applications/Mewla.app 2>/dev/null | cut -d';' -f1)"
say "version: $(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' /Applications/Mewla.app/Contents/Info.plist)"
if [[ "$src" == http* ]]; then
  check "installed version matches the stable tag" test "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' /Applications/Mewla.app/Contents/Info.plist)" = "${PUBLIC_RELEASE_TAG#v}"
  check "installed updater uses the public release repository" python3 -I -c 'import json,sys; c=json.load(open(sys.argv[1])); assert c.get("github")=="daoleno/mewla" and not c.get("url") and c.get("publicKey") and c["version"]==sys.argv[2]' /Applications/Mewla.app/Contents/Resources/nsh/assets/nsh-updater.json "${PUBLIC_RELEASE_TAG#v}"
fi
check "Gatekeeper accepts the installed app (spctl)" spctl -a -t exec -vv /Applications/Mewla.app
codesign -dv --verbose=2 /Applications/Mewla.app 2>&1 | grep -E "Identifier|Authority|TeamIdentifier" | tee -a "$log"
osa -e 'tell application "Finder" to eject disk "Mewla"' >/dev/null 2>&1 || bounded 30 hdiutil detach /Volumes/Mewla >/dev/null 2>&1 || bounded 30 hdiutil detach -force /Volumes/Mewla >/dev/null 2>&1 || true
say "the image is $( [[ -d /Volumes/Mewla ]] && echo "still mounted" || echo ejected)"

# 4. The first launch, as a double-click in Applications: Gatekeeper asks once; the person presses Open.
launch /Applications/Mewla.app
asked=""
for i in $(seq 1 60); do
  # The question first: while it is up the app's process exists but has no window (its state is not always T).
  q=$(answer_gatekeeper)
  if [[ -n "$q" && "$q" == *"::"* ]]; then
    asked="$q"; sleep 1; shot 02-gatekeeper-asks
    say "Gatekeeper asked: $q"
    say "pressed Open in: $(press_open)"
    break
  fi
  up_with_window && break
  [[ $((i % 10)) == 0 ]] && say "waiting for the first launch: app pid $(app_pid) state $(ps -o state= -p "$(app_pid)" 2>/dev/null | tr -d ' ')"
  sleep 1
done
[[ -n "$asked" ]] || say "no first-launch question was seen"
for i in $(seq 1 60); do app_up && has_window && break; sleep 1; done
check "the app is running with a window" up_with_window
sleep 1; shot 03-first-window
for i in $(seq 1 120); do health >/dev/null && break; sleep 1; done
check "this Mac's Mewla answers (/health)" health
health | tee "$out/health-1.json" >> "$log"; echo >> "$log"
launchctl print "gui/$(id -u)/com.daoleno.mewla.daemon" 2>/dev/null | grep -E "program identifier|state =|pid =" | tee -a "$log"
# The first run settles (the waking cat, then ready).
sleep 15
both 04-first-run

# 5. Pair a phone…: the menu bar cat's menu (a right-click), its row, the pairing window with the link.
pos=$(osa -e "tell application \"System Events\" to tell (first process whose unix id is $(app_pid)) to get {position, size} of menu bar item 1 of menu bar 2" 2>&1 || true)
say "the cat in the menu bar: $pos"
read -r x y w h < <(tr -d ',' <<<"$pos")
paired_by=""
if [[ "$x" =~ ^[0-9]+$ ]]; then
  bounded 90 swift "$here/fidelity/click.swift" right $((x + w / 2)) $((y + h / 2)) || true
  sleep 2
  shot 05-cat-menu
  # The open menu by the keyboard, as a person can: Open Mewla, Quick chat, (a separator), Pair a phone…, Return.
  # (An Accessibility press on a status item's row, or a synthetic click on it, reports success and does nothing.)
  key() { osa -e "tell application \"System Events\" to key code $1" >/dev/null 2>&1 || true; sleep 0.3; }
  key 125; key 125; key 125
  shot 05-cat-menu-row
  key 36
  paired_by="the cat's menu"
fi
sleep 2
swiftc "$here/check-pair-window.swift" -o "$RUNNER_TEMP/check-pair-window" || exit 1
if bun "$here/lan-phone-walk.ts" "$RUNNER_TEMP/check-pair-window" "$out" >> "$log" 2>&1; then
  say "ok: consent, exact displayed QR, phone WebSocket after dismissal, restart and LAN off"
else
  say "FAILED: trusted LAN phone walk"
  grep -E '^Decoded QR: url=(true|false), characters=[0-9]+, bytes=[0-9]+, declaredAddressBytes=-?[0-9]+, version=([0-9]+|undefined)$' "$log" || true
  grep -E '^error: QR rejected: url=(true|false), bytes=[0-9]+, declaredAddressBytes=-?[0-9]+, version=([0-9]+|undefined), matchesClipboard=(true|false), clipboardParses=(true|false)$' "$log" || true
  failed+=("trusted LAN phone walk")
  # Close the QR window before resuming any public screenshots.
  quit_app || exit 1
fi

# 6. Quit Mewla, then open it again: no question this time, the daemon kept running, the window back.
check "Quit closes the app" quit_app
check "the daemon keeps running after Quit" health
launch /Applications/Mewla.app
sleep 3
q=$(answer_gatekeeper)
[[ -z "$q" ]] && say "ok: no Gatekeeper question on the second launch" || { say "FAILED: asked again: $q"; failed+=("asked again"); }
for i in $(seq 1 60); do app_up && has_window && break; sleep 1; done
check "the second launch has its window" up_with_window
sleep 10
both 07-relaunch
health | tee "$out/health-2.json" >> "$log"; echo >> "$log"
python3 -I -c 'import json,sys; a,b=(json.load(open(p)) for p in sys.argv[1:3]); assert a["daemon_id"]==b["daemon_id"]; print("same daemon_id across Quit and relaunch")' "$out/health-1.json" "$out/health-2.json" >> "$log" 2>&1 || { say "FAILED: daemon_id changed"; failed+=("daemon_id"); }

# Populate the installed daemon through its CLI. These named Sessions only wait on the CI Mac.
cli=/Applications/Mewla.app/Contents/Helpers/mewla
hold="$RUNNER_TEMP/hold-session.sh"
printf '#!/bin/sh\nexec sleep 1800\n' > "$hold"
chmod 700 "$hold"
for name in 'Review the calendar' 'Check desktop layout' 'Prepare release notes'; do
  check "create sample Session: $name" "$cli" worker spawn -name "$name" -executor claude -command "$hold" -cwd "$RUNNER_TEMP"
done
for item in '10:00 Review release notes' '15:00 Check desktop layout' '23:55 Publish the preview'; do
  check "create Calendar item: ${item#* }" "$cli" calendar create -kind reminder -date "$(date +%F)" -timezone UTC -time "${item%% *}" -title "${item#* }"
done

# 7. Capture the signed app at desktop size, including the populated Home and Calendar.
for surface in 'busy-home {}' 'settings-general {"settings":"general"}' 'settings-access {"settings":"access"}' 'calendar {"open":"calendar"}'; do
  name="${surface%% *}"; script="${surface#* }"
  quit_app || true
  launch -n --env MEWLA_APP_WINDOW_SIZE=1280x800 --env MEWLA_APP_DEV_SCRIPT="$script" /Applications/Mewla.app
  for i in $(seq 1 60); do app_up && has_window && break; sleep 1; done
  sleep 10
  both "08-$name"
done
quit_app || true

rm -f "$gatekeeper_scpt"
say "checks failed: ${#failed[@]}${failed:+ (${failed[*]})}"
[[ ${#failed[@]} -eq 0 ]]
