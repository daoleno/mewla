#!/usr/bin/env bash
# Mewla for Mac's permission flow walked on a CI Mac (macos-permissions.yml), the release app installed in
# /Applications with its daemon as the app's LaunchAgent. The system TCC.db (writable on GitHub's images, SIP off)
# stands in for what a person does in System Settings: a row switched off (Deny), a row removed (−), a row on (the
# drag or the switch). A runner cannot drag into System Settings or answer macOS's own prompt.
#   client/macos/scripts/permissions-walk.sh <Mewla.app> <out dir>
# Writes <out>/walk.txt (each phase, the daemon's grants, the process tree), <out>/NN-*.png (the screen), the app's and
# the daemon's logs, and tccd's lines for Mewla.
set -euo pipefail
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
walk="$out/walk.txt"
: > "$walk"
say() { echo "== $*" | tee -a "$walk"; }

sudo rm -rf "$app"
ditto "$src" "$app"
codesign -d -r- "$app" 2>/dev/null | sed -n 's/^#* *designated => //p' > "$work/req.txt"
csreq -r "$work/req.txt" -b "$work/req.bin"
app_req=$(xxd -p "$work/req.bin" | tr -d '\n')

tcc_reload() { sudo killall tccd 2>/dev/null || true; killall tccd 2>/dev/null || true; sleep 2; }
clear_rows() { sudo sqlite3 "$db" "delete from access where client = 'com.daoleno.mewla' or client like '$app/%';"; }
# A row for Mewla.app (what dragging it in, or + and choosing it, writes): service, auth (0 off, 2 on).
row() {
  sudo sqlite3 "$db" "insert or replace into access (service, client, client_type, auth_value, auth_reason, auth_version, csreq, flags, last_modified) values ('$1', 'com.daoleno.mewla', 0, $2, 3, 1, X'$app_req', 0, strftime('%s','now'));"
}
drop_row() { sudo sqlite3 "$db" "delete from access where service = '$1' and client = 'com.daoleno.mewla';"; }
rows() { sudo sqlite3 "$db" "select service, client, client_type, auth_value from access where client = 'com.daoleno.mewla' or client like '$app/%';"; }

# The daemon's own view (through its control socket: never this shell's, which the runner pre-grants).
grants() {
  "$cli" agent-use status --json 2>/dev/null | python3 -I -c '
import json, sys
try:
    status = json.load(sys.stdin)
except Exception:
    print("(no answer)"); sys.exit(0)
print(" ".join(f"{p[\"id\"]}={p[\"state\"]}" for p in (status.get("computer") or {}).get("permissions") or []) or "(none)")'
}
note_grants() { say "daemon: $(grants)"; }
wait_grant() { # id state seconds
  for _ in $(seq 1 $(( $3 * 2 ))); do grants | grep -q "$1=$2" && return 0; sleep 0.5; done
  return 1
}
gui_pid() { pgrep -f "^$app/Contents/MacOS/Mewla$" || true; }
quit_gui() {
  osascript -e 'tell application id "com.daoleno.mewla" to quit' >/dev/null 2>&1 || true
  for _ in $(seq 1 40); do [[ -z "$(gui_pid)" ]] && break; sleep 0.5; done
  [[ -z "$(gui_pid)" ]] || kill "$(gui_pid)" 2>/dev/null || true
}
quit_settings() { osascript -e 'tell application id "com.apple.systempreferences" to quit' >/dev/null 2>&1 || true; sleep 1; }
app_log() { cat "$data"/logs/* 2>/dev/null || true; }
open_gui() { # devScript JSON
  quit_gui
  open --env "MEWLA_APP_DATA_DIR=$data" --env "MEWLA_APP_DEV_SCRIPT=$1" "$app"
  for _ in $(seq 1 120); do app_log | grep -q "dev script: connected" && return 0; sleep 1; done
  say "the app did not connect"
}
connected_count() { app_log | grep -c "dev script: connected" || true; }
open_and_wait() { # devScript JSON, log line to wait for
  local before; before=$(app_log | grep -c "$2" || true)
  open_gui "$1"
  for _ in $(seq 1 60); do [[ $(app_log | grep -c "$2" || true) -gt $before ]] && return 0; sleep 1; done
  say "no '$2' in the app's log"
}
shot() { screencapture -x "$out/$1.png" || true; }

say "install: $(defaults read "$app/Contents/Info.plist" CFBundleShortVersionString)"
clear_rows; tcc_reload
open_gui '{}'
for _ in $(seq 1 90); do "$cli" agent-use status --json >/dev/null 2>&1 && break; sleep 1; done
say "processes (the daemon is the child of the app's own executable):"
ps -axo pid,ppid,command | grep -E "Mewla.app/Contents/(MacOS|Helpers)" | grep -v grep | tee -a "$walk"
launchctl print "gui/$(id -u)/com.daoleno.mewla.daemon" 2>/dev/null | grep -E "program|state =|pid =" | tee -a "$walk" || true
note_grants

for permission in accessibility screen_recording; do
  service=kTCCServiceAccessibility; [[ $permission == screen_recording ]] && service=kTCCServiceScreenCapture
  n=$([[ $permission == accessibility ]] && echo 1 || echo 5)

  say "$permission: never asked (no row)"
  drop_row "$service"; tcc_reload; quit_settings
  wait_grant "$permission" missing 20 || true; note_grants
  open_and_wait '{"settings":"access"}' "dev script: connected"; sleep 4; shot "$n-$permission-0-never-asked-settings"
  open_and_wait "{\"settings\":\"access\",\"allow\":\"$permission\"}" "dev script: allow $permission"; sleep 5
  shot "$n-$permission-1-never-asked-panel"

  say "$permission: denied (row off)"
  quit_settings; row "$service" 0; tcc_reload
  wait_grant "$permission" off 20 || true; note_grants
  open_and_wait "{\"settings\":\"access\",\"allow\":\"$permission\"}" "dev script: allow $permission"; sleep 5
  shot "$n-$permission-2-denied-panel"

  say "$permission: row removed (−)"
  quit_settings; drop_row "$service"; tcc_reload
  wait_grant "$permission" missing 20 || true; note_grants
  open_and_wait "{\"settings\":\"access\",\"allow\":\"$permission\"}" "dev script: allow $permission"; sleep 5
  shot "$n-$permission-3-removed-panel"

  say "$permission: given (the drag, stood in for by its row), with the panel open and the daemon following"
  row "$service" 2; tcc_reload
  sleep 1; shot "$n-$permission-4-given"; note_grants
  if wait_grant "$permission" granted 60; then say "$permission: granted in the daemon"; else say "$permission: NOT granted after 60 s"; fi
  note_grants
  sleep 3; shot "$n-$permission-5-granted"
  app_log | grep -E "dev script: (grants|allow)" | tail -12 | tee -a "$walk"
done

say "rows now:"; rows | tee -a "$walk"
say "the daemon's log (grants, restarts):"
grep -h -E "grants:|screen:|restart" "$HOME/.mewla/logs/"*.log 2>/dev/null | tail -40 | tee -a "$walk" || true

# Item 3 on the real binaries: Mewla.app's row only (what + Mewla.app writes), the bare helper as its own job (0.4.24)
# against the app's executable as the job with the helper its child (this change).
say "one row each for Mewla.app; the old job shape against the new"
clear_rows; row kTCCServiceAccessibility 2; row kTCCServiceScreenCapture 2; tcc_reload
shape() { # label program args...
  local label=$1; shift
  local plist="$work/$label.plist"
  python3 -I - "$plist" "$label" "$out/shape-$label.json" "$@" <<'PY'
import plistlib, sys
path, label, out, *argv = sys.argv[1:]
plistlib.dump({"Label": "com.daoleno.mewla.walk." + label, "ProgramArguments": argv, "StandardOutPath": out,
               "RunAtLoad": True, "LimitLoadToSessionType": "Aqua", "ProcessType": "Interactive"}, open(path, "wb"))
PY
  rm -f "$out/shape-$label.json"
  launchctl bootstrap "gui/$(id -u)" "$plist"
  for _ in $(seq 1 30); do [[ -s "$out/shape-$label.json" ]] && break; sleep 0.5; done
  launchctl bootout "gui/$(id -u)/com.daoleno.mewla.walk.$label" 2>/dev/null || true
  say "$label: $(cat "$out/shape-$label.json" 2>/dev/null || echo '(no answer)')"
}
shape old-bare-helper "$cli" screen preflight
shape new-app-host "$app/Contents/MacOS/Mewla" --daemon screen preflight

log show --last 40m --style compact --predicate 'process == "tccd" AND eventMessage CONTAINS[c] "mewla"' 2>/dev/null | tail -600 > "$out/tccd.log" || true
cp "$data"/logs/* "$out/" 2>/dev/null || true
cp "$HOME/.mewla/logs/"*.log "$out/" 2>/dev/null || true
quit_gui; quit_settings
