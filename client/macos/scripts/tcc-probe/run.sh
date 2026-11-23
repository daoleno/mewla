#!/usr/bin/env bash
# Which identity macOS charges for Screen Recording and Accessibility, per launch shape, on a CI Mac (system TCC.db
# writable with sudo on GitHub's macOS images). A probe bundle /Applications/TCCProbe.app (com.daoleno.mewla.tccprobe)
# carries a launcher (Contents/MacOS) that spawns a bare helper (Contents/Helpers/probe, identifier ….daemon), the
# layout of Mewla.app. Shapes:
#   L  a LaunchAgent whose program is the bare helper (Mewla 0.4.24's daemon job)
#   A  a LaunchAgent whose program is the bundle's executable, which spawns the helper
#   O  the bundle opened by LaunchServices (`open`), which spawns the helper (Mewla Dev's daemon, the app's child)
#   S  the helper run from this step's shell (charged to the runner's pre-granted shell: the trap)
# Rows: none, the bundle (what "+ Mewla.app" in System Settings writes), the helper by path, the helper by identifier,
# the bundle denied. Then the request APIs from L and A with no row, and with a denied row.
#   client/macos/scripts/tcc-probe/run.sh <out dir>
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo "this script edits the system TCC.db: a CI Mac only" >&2; exit 2; }
here="$(cd "$(dirname "$0")" && pwd)"
out="${1:?out dir}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
db="/Library/Application Support/com.apple.TCC/TCC.db"
id=com.daoleno.mewla.tccprobe
app=/Applications/TCCProbe.app
results="$out/results.jsonl"
: > "$results"

{
  sw_vers; csrutil status || true; id
  echo "--- schema"; sudo sqlite3 "$db" ".schema access"
  echo "--- runner's existing ScreenCapture/Accessibility rows"
  sudo sqlite3 "$db" "select service, client, client_type, auth_value from access where service in ('kTCCServiceScreenCapture','kTCCServiceAccessibility','kTCCServicePostEvent') order by service, client;"
} > "$out/system.txt" 2>&1

cc -O2 -o "$work/probe" "$here/probe.c" -framework CoreGraphics -framework ApplicationServices -framework CoreFoundation
cc -O2 -o "$work/launcher" "$here/launcher.c"
sudo rm -rf "$app"
mkdir -p "$work/TCCProbe.app/Contents/MacOS" "$work/TCCProbe.app/Contents/Helpers"
cp "$work/launcher" "$work/TCCProbe.app/Contents/MacOS/TCCProbe"
cp "$work/probe" "$work/TCCProbe.app/Contents/Helpers/probe"
cat > "$work/TCCProbe.app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>$id</string>
  <key>CFBundleName</key><string>TCCProbe</string>
  <key>CFBundleExecutable</key><string>TCCProbe</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST
codesign --force --sign - --identifier "$id.daemon" "$work/TCCProbe.app/Contents/Helpers/probe"
codesign --force --sign - "$work/TCCProbe.app"
cp -R "$work/TCCProbe.app" "$app"
codesign -dvvv "$app" > "$out/codesign-app.txt" 2>&1; codesign -d -r- "$app" >> "$out/codesign-app.txt" 2>&1
codesign -dvvv "$app/Contents/Helpers/probe" > "$out/codesign-helper.txt" 2>&1; codesign -d -r- "$app/Contents/Helpers/probe" >> "$out/codesign-helper.txt" 2>&1

csreq_hex() {
  codesign -d -r- "$1" 2>/dev/null | sed -n 's/^#* *designated => //p' > "$work/req.txt"
  csreq -r "$work/req.txt" -b "$work/req.bin"
  xxd -p "$work/req.bin" | tr -d '\n'
}
app_req=$(csreq_hex "$app")
helper_req=$(csreq_hex "$app/Contents/Helpers/probe")

tcc_reload() { sudo killall tccd 2>/dev/null || true; killall tccd 2>/dev/null || true; sleep 2; }
clear_rows() { sudo sqlite3 "$db" "delete from access where client like '$id%' or client like '$app/%';"; }
row() { # service client client_type auth_value csreq_hex
  sudo sqlite3 "$db" "insert or replace into access (service, client, client_type, auth_value, auth_reason, auth_version, csreq, flags, last_modified) values ('$1', '$2', $3, $4, 3, 1, X'$5', 0, strftime('%s','now'));"
}
rows() { # client client_type auth_value csreq_hex
  for service in kTCCServiceScreenCapture kTCCServiceAccessibility; do row "$service" "$@"; done
}
dump_rows() { sudo sqlite3 "$db" "select service, client, client_type, auth_value, auth_reason from access where client like '$id%' or client like '$app/%';"; }

agent() { # shape label mode program
  local shape=$1 label=$2 mode=$3 program=$4 job="$id.$1"
  cat > "$work/$job.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$job</string>
  <key>ProgramArguments</key><array><string>$program</string><string>$mode</string><string>$shape/$label</string></array>
  <key>EnvironmentVariables</key><dict><key>PROBE_OUT</key><string>$results</string></dict>
  <key>AssociatedBundleIdentifiers</key><array><string>$id</string></array>
  <key>RunAtLoad</key><true/>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
  <key>ProcessType</key><string>Interactive</string>
</dict></plist>
PLIST
  launchctl bootout "gui/$(id -u)/$job" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$work/$job.plist"
  for _ in $(seq 1 40); do grep -q "\"$shape/$label\"" "$results" && break; sleep 0.5; done
  grep -q "\"$shape/$label\"" "$results" || echo "{\"label\":\"$shape/$label\",\"missing\":true}" >> "$results"
  launchctl bootout "gui/$(id -u)/$job" 2>/dev/null || true
}
shapes() { # label mode
  agent L "$1" "$2" "$app/Contents/Helpers/probe"
  agent A "$1" "$2" "$app/Contents/MacOS/TCCProbe"
  open -n -W --env "PROBE_OUT=$results" "$app" --args "$2" "O/$1" || echo "{\"label\":\"O/$1\",\"missing\":true}" >> "$results"
  PROBE_OUT="$results" "$app/Contents/Helpers/probe" "$2" "S/$1"
}

clear_rows; tcc_reload; shapes none check
clear_rows; rows "$id" 0 2 "$app_req"; tcc_reload; shapes bundle check
clear_rows; rows "$app/Contents/Helpers/probe" 1 2 "$helper_req"; tcc_reload; shapes helper-path check
clear_rows; rows "$id.daemon" 0 2 "$helper_req"; tcc_reload; shapes helper-id check
clear_rows; rows "$id" 0 0 "$app_req"; tcc_reload; shapes bundle-denied check
# A grant under an older signature (the ad hoc requirement of another build): the row is there, switched on.
clear_rows; rows "$id" 0 2 "$helper_req"; tcc_reload; shapes bundle-stale-req check
# The same stale row switched off, and Accessibility's stale row beside an older daemon's path row switched off (as
# Mewla 0.4.25 leaves it): what the list shows for the bundle, against what the preflight says.
clear_rows; rows "$id" 0 0 "$helper_req"; tcc_reload; shapes bundle-stale-req-off check
clear_rows; rows "$id" 0 2 "$helper_req"; row kTCCServiceAccessibility "$app/Contents/Helpers/probe" 1 0 "$helper_req"; tcc_reload; shapes bundle-stale-req-path-off check

# What asking does: no row (never asked, or removed with −), then a denied row.
for shape in L A; do
  program="$app/Contents/Helpers/probe"; [[ $shape == A ]] && program="$app/Contents/MacOS/TCCProbe"
  clear_rows; tcc_reload
  agent "$shape" ask-no-row request "$program"
  screencapture -x "$out/ask-no-row-$shape.png" || true
  { echo "--- rows after $shape asks with no row"; dump_rows; } >> "$out/asks.txt"
  killall universalAccessAuthWarn UserNotificationCenter 2>/dev/null || true
  clear_rows; rows "$id" 0 0 "$app_req"; tcc_reload
  agent "$shape" ask-denied request "$program"
  screencapture -x "$out/ask-denied-$shape.png" || true
  { echo "--- rows after $shape asks with a denied bundle row"; dump_rows; } >> "$out/asks.txt"
  killall universalAccessAuthWarn UserNotificationCenter 2>/dev/null || true
done

cp "$results.info" "$out/info.txt" 2>/dev/null || true
log show --last 15m --style compact --predicate 'process == "tccd"' 2>/dev/null | grep -i tccprobe | tail -400 > "$out/tccd.log" || true
clear_rows; tcc_reload
sudo rm -rf "$app"
python3 -I - "$results" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
print(f"{'case':28} {'screen':6} {'ax':6} tcc_screen tcc_ax pid ppid")
for r in rows:
    if r.get("missing"):
        print(f"{r['label']:28} (no answer)")
        continue
    print(f"{r['label']:28} {str(r['screen']):6} {str(r['ax']):6} {r['tcc_screen']:10} {r['tcc_ax']:6} {r['pid']} {r['ppid']}")
PY
