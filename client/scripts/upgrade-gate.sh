#!/usr/bin/env bash
# The in-place upgrade gate on an Android emulator that is already booted (adb sees one device):
#   1. the released Expo app (OLD_APK) pairs with a fresh daemon and keeps a typed draft;
#   2. the NSH app (NEW_APK, same package, same signing key) installs over it with `install -r`;
#   3. it opens without pairing, signs in with the same device key (the daemon lists one device,
#      seen again after the upgrade) and holds the draft;
#   4. its scanner opens in this release build: the camera prompt, then the camera running, the app up.
# Inputs: OLD_APK, NEW_APK, MEWLA (daemon binary), OUT (evidence directory). Exit 0 only if all hold.
set -euo pipefail
: "${OLD_APK:?}" "${NEW_APK:?}" "${MEWLA:?}" "${OUT:?}"
PKG=com.daoleno.mewla
PORT="${PORT:-9941}"
DRAFT="upgrade gate draft"
mkdir -p "$OUT"
state="$(mktemp -d)"
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/gate.log"; }
fail() { log "FAIL: $*"; exit 1; }
shot() { adb exec-out screencap -p > "$OUT/$1.png" 2>/dev/null || true; adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1 && adb shell cat /sdcard/ui.xml > "$OUT/$1.xml" || true; }
screen() { adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1; adb shell cat /sdcard/ui.xml; }
# Taps the first node whose text, description or class matches $1 (case-insensitive regex), waiting up to $2 s.
tap() {
  local deadline=$((SECONDS + ${2:-20})) at
  while ((SECONDS < deadline)); do
    at=$(screen | python3 -c '
import re, sys
xml, pattern = sys.stdin.read(), re.compile(sys.argv[1], re.I)
for node in re.finditer(r"<node [^>]*>", xml):
    n = node.group(0)
    values = re.findall(r"(?:text|content-desc|class)=\"([^\"]*)\"", n)
    box = re.search(r"bounds=\"\[(\d+),(\d+)\]\[(\d+),(\d+)\]\"", n)
    if box and any(pattern.search(v) for v in values):
        x1, y1, x2, y2 = map(int, box.groups()); print((x1 + x2) // 2, (y1 + y2) // 2); break
' "$1")
    if [[ -n "$at" ]]; then adb shell input tap $at; return 0; fi
    sleep 1
  done
  return 1
}
shows() { screen | grep -qiE "$1"; }
devices() { env -i HOME="$state/home" PATH="$PATH" "$MEWLA" devices list -json -state-dir "$state/mewla"; }

log "emulator ABIs: $(adb shell getprop ro.product.cpu.abilist | tr -d '\r')"
mkdir -p "$state/home" "$state/bin"
# A daemon of its own: own state, own tmux server, nothing from this shell, and an inert Brain host:
# a cursor-agent that answers nothing, so Brain starts (the Expo app shows its composer) and never replies.
printf '#!/bin/sh\n[ "$1" = --version ] && { echo 0.0.0-gate; exit 0; }\nexec sleep 86400\n' > "$state/bin/cursor-agent"
chmod +x "$state/bin/cursor-agent"
tmux -S "$state/tmux.sock" new-session -d -s gate
env -i HOME="$state/home" PATH="$state/bin:$PATH" TERM=xterm-256color MEWLA_BRAIN_HOST_EXECUTOR=agent TMUX="$state/tmux.sock,0,0" \
  "$MEWLA" serve -addr "0.0.0.0:$PORT" -state-dir "$state/mewla" > "$OUT/daemon.log" 2>&1 &
daemon=$!
# The trap's last status becomes the script's: a cleanup that trips (the daemon still writing as
# its state is removed) must not turn a PASS into a failure.
trap 'kill "$daemon" 2>/dev/null || true; wait "$daemon" 2>/dev/null || true; tmux -S "$state/tmux.sock" kill-server 2>/dev/null || true; rm -rf "$state" 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && break; sleep 1; done
link=$(env -i HOME="$state/home" PATH="$PATH" "$MEWLA" pair -details -state-dir "$state/mewla" "http://10.0.2.2:$PORT" 2>&1 | grep -o 'mewla://settings?p=[^ ]*' | head -1)
[[ -n "$link" ]] || fail "the daemon gave no pairing link"

log "1. the Expo app pairs and keeps a draft"
adb uninstall "$PKG" > /dev/null 2>&1 || true
adb logcat -c
adb install "$OLD_APK" | tail -1 | tee -a "$OUT/gate.log"
adb shell pm grant "$PKG" android.permission.POST_NOTIFICATIONS 2>/dev/null || true
adb shell am start -W -a android.intent.action.VIEW -d "$link" "$PKG" > /dev/null
tap "^pair and grant access$" 60 || { shot 1-no-confirm; fail "the Expo app did not ask to pair"; }
for _ in $(seq 1 60); do [[ "$(devices | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["devices"] or []))')" == 1 ]] && break; sleep 1; done
before=$(devices)
device_id=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["devices"][0]["id"])' "$before")
log "paired as device $device_id"
shot 1-paired
# v0.3.0 could die on an unhandled "Daemon is not connected." right after pairing (fixed in v0.3.1); it is paired by then.
sleep 3
if [[ -z "$(adb shell pidof "$PKG" | tr -d '\r')" ]]; then
  adb logcat -d -b crash > "$OUT/old-app-crash.txt"
  log "the Expo app quit after pairing ($(grep -m1 -o 'JavascriptException: .*' "$OUT/old-app-crash.txt" || echo 'see old-app-crash.txt')); opening it again"
  adb shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 > /dev/null 2>&1
fi
# The Expo app can still be settling Brain when the composer first takes focus, and the typed text
# is lost; type it again until the composer shows it.
for attempt in 1 2 3; do
  tap "android.widget.EditText" 60 || { shot 1-no-composer; fail "no composer to type the draft in"; }
  sleep 1
  adb shell input text "${DRAFT// /%s}"
  sleep 1
  shows "$DRAFT" && break
  log "the draft did not land in the Expo app's composer (try $attempt)"
done
shows "$DRAFT" || { shot 1-no-draft; fail "the Expo app's composer did not take the draft"; }
sleep 3 # the draft is written 400 ms after typing stops
shot 1-draft
adb shell am force-stop "$PKG"
sleep 2

log "2. the NSH app installs over it"
install=$(adb install -r "$NEW_APK" 2>&1 | tail -1 || true)
log "install -r: $install"
[[ "$install" == Success* ]] || fail "install over the Expo app refused (INSTALL_FAILED_UPDATE_INCOMPATIBLE means another signing key)"
seen_before=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["devices"][0].get("last_seen_at",""))' "$before")
sleep 2

log "3. it opens paired, signs in with the same key and holds the draft"
adb shell am start -W -n "$PKG/.MainActivity" > /dev/null
sleep 10
shot 3-opened
shows "Pair a computer" && fail "the NSH app asks to pair again"
tap "^More$" 10 || true
for _ in $(seq 1 30); do shows "· Connected" && break; sleep 1; done
shot 3-more
shows "· Connected" || fail "the NSH app does not show the computer as connected"
log "the app shows the computer as Connected"
tap "^Brain$" 10 || true
for _ in $(seq 1 60); do shows "$DRAFT" && break; sleep 1; done
shot 3-brain-draft
shows "$DRAFT" || fail "Brain's composer does not show the draft typed in the Expo app"
log "Brain's composer shows the draft"
after=$(devices)
python3 - "$before" "$after" "$seen_before" "$device_id" <<'PY' | tee -a "$OUT/gate.log"
import json, sys
before, after, seen_before, device_id = json.loads(sys.argv[1]), json.loads(sys.argv[2]), sys.argv[3], sys.argv[4]
ids = [d["id"] for d in after["devices"]]
assert ids == [device_id], f"devices after the upgrade: {ids}, expected only {device_id}"
seen_after = after["devices"][0].get("last_seen_at", "")
assert seen_after > seen_before, f"device not seen again after the upgrade ({seen_before} -> {seen_after})"
print(f"same device {device_id}, seen again at {seen_after}")
PY
grep -q "device=$device_id" "$OUT/daemon.log" && log "daemon log: connection from $device_id"
adb root > /dev/null 2>&1 || true
sleep 2
storage=$(adb shell cat "/data/data/$PKG/files/nsh/$PKG/storage.json" 2>/dev/null || adb shell run-as "$PKG" cat "files/nsh/$PKG/storage.json")
echo "$storage" | python3 -c '
import json, sys
data = json.load(sys.stdin)
keys = sorted(data)
print("NSH storage keys:", ", ".join(k.split(":")[0] + ":…" if k.startswith("mewla.draft") else k for k in keys))
drafts = [v for k, v in data.items() if k.startswith("mewla.draft.v1:")]
assert any(sys.argv[1] in v for v in drafts), "the draft did not come over"
assert "mewla:v3:servers" in data, "the computers did not come over"
print("draft and computers came over")
' "$DRAFT" | tee -a "$OUT/gate.log"

# The M1 preview died on its first "Scan code" in a release build (R8 had dropped ML Kit's registrar):
# open the scanner in this release build, answer the system's camera prompt, and the app must stay up
# with the camera looking for a code.
log "4. the scanner runs in the release build"
adb logcat -c
adb shell am start -W -a android.intent.action.VIEW -d "mewla://more" "$PKG" > /dev/null
tap "^Settings$" 20 || { shot 4-no-settings; fail "More has no Settings row"; }
tap "^Pair a computer$" 20 || { shot 4-no-pair; fail "Settings has no Pair a computer"; }
tap "^While using the app$" 20 || { shot 4-no-prompt; fail "the scanner did not ask for the camera"; }
for _ in $(seq 1 20); do shows "Looking for a Mewla code" && break; sleep 1; done
sleep 3
shot 4-scanner
if [[ -z "$(adb shell pidof "$PKG" | tr -d '\r')" ]]; then
  adb logcat -d -b crash > "$OUT/scanner-crash.txt"
  fail "the app died with the scanner open (scanner-crash.txt)"
fi
shows "Looking for a Mewla code" || fail "the scanner is not looking for a code"
log "the scanner runs: camera allowed, app up, looking for a code"
log "PASS"
