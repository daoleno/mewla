#!/usr/bin/env bash
# Mewla's grants on the Developer ID build (macos-permissions.yml `signed`, fed by a macos-release.yml proof run):
#   1. the public 0.4.25 installed from its disk image, and Screen Recording given to its Mewla.app (the row System
#      Settings writes for it, its designated requirement);
#   2. this build's notarized disk image put in its place, as a person installs a newer Mewla: the grant is kept, with
#      no new row;
#   3. Accessibility given to Mewla.app the same way: both count.
# The system TCC.db (writable on GitHub's images, SIP off) stands in for System Settings.
#   client/macos/scripts/signed-grants.sh <proof A artifact dir> <out dir>
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo "this script edits the system TCC.db: a CI Mac only" >&2; exit 2; }
signed="${1:?proof artifact dir}"
out="${2:?out dir}"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
db="/Library/Application Support/com.apple.TCC/TCC.db"
app=/Applications/Mewla.app
cli="$app/Contents/Helpers/mewla"
log="$out/signed.txt"
: > "$log"
say() { echo "== $*" | tee -a "$log"; }
tcc_reload() { sudo killall tccd 2>/dev/null || true; killall tccd 2>/dev/null || true; sleep 2; }
rows() { sudo sqlite3 "$db" "select service, client, client_type, auth_value from access where client = 'com.daoleno.mewla' or client like '$app/%';"; }
requirement() { # app → hex csreq of its designated requirement
  codesign -d -r- "$1" 2>/dev/null | sed -n 's/^#* *designated => //p' > "$work/req.txt"
  csreq -r "$work/req.txt" -b "$work/req.bin"
  xxd -p "$work/req.bin" | tr -d '\n'
}
row() { # service auth csreq-hex
  sudo sqlite3 "$db" "insert or replace into access (service, client, client_type, auth_value, auth_reason, auth_version, csreq, flags, last_modified) values ('$1', 'com.daoleno.mewla', 0, $2, 3, 1, X'$3', 0, strftime('%s','now'));"
}
install_dmg() { # dmg
  osascript -e 'tell application id "com.daoleno.mewla" to quit' >/dev/null 2>&1 || true
  for _ in $(seq 1 30); do pgrep -f "^$app/Contents/MacOS/Mewla$" >/dev/null || break; sleep 1; done
  hdiutil attach -readonly -nobrowse -mountpoint "$work/image" "$1" >/dev/null
  sudo rm -rf "$app"
  ditto "$work/image/Mewla.app" "$app"
  hdiutil detach "$work/image" >/dev/null
}
job() { launchctl print "gui/$(id -u)/com.daoleno.mewla.daemon" 2>/dev/null || true; }
# What macOS answers a launchd job of the given program (the daemon's own shape), never this shell (pre-granted).
shape() { # label program args...
  local label=$1; shift
  python3 -I - "$work/$label.plist" "$label" "$out/shape-$label.json" "$@" <<'PY'
import plistlib, sys
path, label, out, *argv = sys.argv[1:]
plistlib.dump({"Label": "com.daoleno.mewla.signed." + label, "ProgramArguments": argv, "StandardOutPath": out,
               "RunAtLoad": True, "LimitLoadToSessionType": "Aqua", "ProcessType": "Interactive"}, open(path, "wb"))
PY
  rm -f "$out/shape-$label.json"
  launchctl bootstrap "gui/$(id -u)" "$work/$label.plist"
  for _ in $(seq 1 30); do [[ -s "$out/shape-$label.json" ]] && break; sleep 0.5; done
  launchctl bootout "gui/$(id -u)/com.daoleno.mewla.signed.$label" 2>/dev/null || true
  say "$label: $(cat "$out/shape-$label.json" 2>/dev/null || echo '(no answer)')"
}
cat > "$work/grants.py" <<'PY'
import json, sys
try:
    status = json.load(sys.stdin)
except Exception:
    print("(no answer)")
    sys.exit(0)
grants = (status.get("computer") or {}).get("permissions") or []
print(" ".join(g["id"] + "=" + g["state"] for g in grants) or "(none)")
PY
grants() { "$cli" agent-use status --json 2>/dev/null | python3 -I "$work/grants.py"; }
wait_grant() { # id state seconds
  for _ in $(seq 1 $(( $3 * 2 ))); do grants | grep -q "$1=$2" && return 0; sleep 0.5; done
  return 1
}

say "1. Mewla 0.4.25 from its release"
gh release download v0.4.25 --repo daoleno/mewla --pattern Mewla-macos-arm64.dmg --dir "$work"
install_dmg "$work/Mewla-macos-arm64.dmg"
say "installed $(defaults read "$app/Contents/Info.plist" CFBundleShortVersionString)"
codesign -dv --verbose=2 "$app" 2>&1 | grep -E "Authority=Developer ID|TeamIdentifier" | tee -a "$log"
old_req=$(requirement "$app")
cp "$work/req.txt" "$out/requirement-0.4.25.txt"
sudo sqlite3 "$db" "delete from access where client = 'com.daoleno.mewla' or client like '$app/%';"
row kTCCServiceScreenCapture 2 "$old_req"; tcc_reload
say "rows: $(rows | tr '\n' ' ')"
open -g "$app"
for _ in $(seq 1 240); do job | grep -q "state = running" && curl -fsS http://127.0.0.1:9876/health >/dev/null 2>&1 && break; sleep 1; done
job | grep -E "program|state =|pid =" | tee -a "$log"
shape old-0425-daemon "$cli" screen preflight

say "2. this build (Developer ID, notarized) in its place"
dmg=$(ls "$signed"/Mewla-*-macos-arm64.dmg | head -1)
spctl -a -t open --context context:primary-signature -vv "$dmg" 2>&1 | tee -a "$log"
xcrun stapler validate "$dmg" | tee -a "$log"
install_dmg "$dmg"
say "installed $(defaults read "$app/Contents/Info.plist" CFBundleShortVersionString)"
spctl -a -t exec -vv "$app" 2>&1 | tee -a "$log"
codesign -dv --verbose=2 "$app" 2>&1 | grep -E "Authority=Developer ID|TeamIdentifier" | tee -a "$log"
new_req=$(requirement "$app")
cp "$work/req.txt" "$out/requirement-new.txt"
if diff "$out/requirement-0.4.25.txt" "$out/requirement-new.txt"; then say "the designated requirement is the same as 0.4.25's"; else say "the designated requirement CHANGED"; fi
open -g "$app"
for _ in $(seq 1 240); do
  job | grep -q "program identifier = Contents/MacOS/Mewla" && job | grep -q "state = running" && curl -fsS http://127.0.0.1:9876/health >/dev/null 2>&1 && break
  sleep 1
done
job | grep -E "program|state =|pid =" | tee -a "$log"
ps -axo pid,ppid,command | grep -E "Mewla.app/Contents/(MacOS|Helpers)" | grep -v grep | tee -a "$log"
for _ in $(seq 1 60); do "$cli" agent-use status --json >/dev/null 2>&1 && break; sleep 1; done
say "rows (none written since 0.4.25's): $(rows | tr '\n' ' ')"
if wait_grant screen_recording granted 60; then say "screen_recording: kept across the update"; else say "screen_recording: NOT kept"; fi
say "daemon: $(grants)"
shape new-app-host "$app/Contents/MacOS/Mewla" --daemon screen preflight

say "3. Accessibility: Allow in Settings › Privacy & access, then given to Mewla.app"
osascript -e 'tell application id "com.daoleno.mewla" to quit' >/dev/null 2>&1 || true
for _ in $(seq 1 30); do pgrep -f "^$app/Contents/MacOS/Mewla$" >/dev/null || break; sleep 1; done
mkdir -p "$work/data"
open --env "MEWLA_APP_DATA_DIR=$work/data" --env 'MEWLA_APP_DEV_SCRIPT={"settings":"access","allow":"accessibility"}' "$app"
for _ in $(seq 1 120); do cat "$work"/data/logs/* 2>/dev/null | grep -q "dev script: allow accessibility" && break; sleep 1; done
sleep 4; screencapture -x "$out/1-allow-panel.png" || true
row kTCCServiceAccessibility 2 "$new_req"; tcc_reload
say "rows: $(rows | tr '\n' ' ')"
if wait_grant accessibility granted 90; then say "accessibility: granted"; else say "accessibility: NOT granted"; fi
sleep 3; screencapture -x "$out/2-both-allowed.png" || true
cat "$work"/data/logs/* 2>/dev/null | grep -E "dev script: (grants|allow|connection)|permission panel" | tee -a "$log" || true
say "daemon: $(grants)"
shape new-app-host-both "$app/Contents/MacOS/Mewla" --daemon screen preflight
grep -h -E "grants:|restart" "$HOME/.mewla/logs/"*.log 2>/dev/null | tail -20 | tee -a "$log" || true
log show --last 30m --style compact --predicate 'process == "tccd" AND eventMessage CONTAINS[c] "mewla"' 2>/dev/null | tail -300 > "$out/tccd.log" || true
