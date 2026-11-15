#!/usr/bin/env bash
# The release upgrade check on an Android emulator that is already booted (adb sees one device): what a
# person on the previous release gets when they install this one over it.
#   1. the previous release's app (OLD_APK) pairs with the previous release's daemon (MEWLA) and keeps a
#      typed draft in Brain's composer;
#   2. this release's app (NEW_APK, same package, same signing key, a higher versionCode) installs over it
#      with `install -r`;
#   3. it opens without pairing, shows the computer Connected, signs in with the same device key (the
#      daemon lists one device, seen again after the upgrade) and holds the draft, in the UI and in storage;
#   4. its scanner runs in this release build: the camera prompt, then the camera looking for a code.
# release-artifacts.yml runs it before anything is published. Inputs: OLD_APK, NEW_APK, MEWLA (daemon
# binary), OUT (evidence directory); MEWLA_UPGRADE_SAME_VERSION_OK=1 accepts an equal versionCode (a
# build-only candidate). Exit 0 only if all of it holds.
set -euo pipefail
: "${OLD_APK:?}" "${NEW_APK:?}" "${MEWLA:?}" "${OUT:?}"
PKG=com.daoleno.mewla
PORT="${PORT:-9942}"
DRAFT="release upgrade draft"
mkdir -p "$OUT"
state="$(mktemp -d)"
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/upgrade.log"; }
fail() { log "FAIL: $*"; shot fail; exit 1; }
screen() {
  # A failed dump leaves the previous file: remove it first so a stale screen is never read.
  adb shell rm -f /sdcard/ui.xml
  adb shell uiautomator dump /sdcard/ui.xml > /dev/null 2>&1 || true
  adb exec-out cat /sdcard/ui.xml 2>/dev/null || true
}
shot() { adb exec-out screencap -p > "$OUT/$1.png" 2>/dev/null || true; screen > "$OUT/$1.xml" || true; }
# The centre of the last node whose text, content-desc or resource-id is exactly $1 (covered pages of a
# navigation stack come first in the dump), else of the last one that contains it, case-insensitive;
# "re:<pattern>" matches the pattern instead.
find_node() {
  python3 -I -c '
import re, sys
xml, want = sys.stdin.read(), sys.argv[1]
exact, partial = None, None
for node in re.finditer(r"<node [^>]*>", xml):
    n = node.group(0)
    values = [v for k, v in re.findall(r"\b(text|content-desc|resource-id)=\"([^\"]*)\"", n)]
    box = re.search(r"bounds=\"\[(\d+),(\d+)\]\[(\d+),(\d+)\]\"", n)
    if not box:
        continue
    x1, y1, x2, y2 = map(int, box.groups())
    at = f"{(x1 + x2) // 2} {(y1 + y2) // 2}"
    if want.startswith("re:"):
        if any(re.search(want[3:], v) for v in values):
            exact = at
    elif want in values:
        exact = at
    elif any(want.lower() in v.lower() for v in values):
        partial = at
if exact or partial:
    print(exact or partial)
' "$1"
}
# Taps the node $1 names, waiting up to $2 s for it.
tap() {
  local deadline=$((SECONDS + ${2:-20})) at
  while ((SECONDS < deadline)); do
    at=$(screen | find_node "$1")
    if [[ -n "$at" ]]; then adb shell input tap $at; return 0; fi
    sleep 1
  done
  return 1
}
# Waits up to $2 s for a node $1 names.
shows() {
  local deadline=$((SECONDS + ${2:-1}))
  while ((SECONDS < deadline)); do
    [[ -n "$(screen | find_node "$1")" ]] && return 0
    sleep 1
  done
  return 1
}
version_code() { adb shell dumpsys package "$PKG" | grep -m1 -o 'versionCode=[0-9]*' | cut -d= -f2; }
devices() { env -i HOME="$state/home" PATH="$PATH" "$MEWLA" devices list -json -state-dir "$state/mewla"; }

log "emulator ABIs: $(adb shell getprop ro.product.cpu.abilist | tr -d '\r'); daemon: $("$MEWLA" version 2>/dev/null | head -1)"
mkdir -p "$state/home" "$state/bin"
# The released daemon it upgrades from may still run Sessions in tmux: a private tmux server (-S).
# A daemon of its own: own state, own tmux server, nothing from this shell, and an inert Brain host:
# a cursor-agent that answers nothing, so Brain starts (its composer shows) and never replies.
printf '#!/bin/sh\n[ "$1" = --version ] && { echo 0.0.0-upgrade; exit 0; }\nexec sleep 86400\n' > "$state/bin/cursor-agent"
# No systemd-run: the daemon's session host is then its own child, never a mewla-session-host-* user unit.
printf '#!/bin/sh\necho "systemd-run is off for the release upgrade daemon" >&2\nexit 1\n' > "$state/bin/systemd-run"
chmod +x "$state/bin/cursor-agent" "$state/bin/systemd-run"
tmux -S "$state/tmux.sock" new-session -d -s upgrade
# On loopback: the emulator reaches it as 10.0.2.2. MEWLA_NO_AUTOUPDATE: a released daemon older than
# Latest would update itself and restart in the middle of the check.
env -i HOME="$state/home" PATH="$state/bin:$PATH" TERM=xterm-256color MEWLA_BRAIN_HOST_EXECUTOR=agent MEWLA_NO_AUTOUPDATE=1 TMUX="$state/tmux.sock,0,0" \
  "$MEWLA" serve -addr "127.0.0.1:$PORT" -state-dir "$state/mewla" > "$OUT/daemon.log" 2>&1 &
daemon=$!
# The session host outlives its daemon: stop it and its Sessions by the pids it reports.
stop_host() {
  env -i HOME="$state/home" PATH="$PATH" "$MEWLA" host status --state-dir "$state/mewla" --json > "$state/host.json" 2>/dev/null || return 0
  python3 -c 'import json, sys
d = json.load(open(sys.argv[1]))
print(" ".join(str(s["pid"]) for s in d.get("sessions") or []), (d.get("host") or {}).get("pid", ""))' "$state/host.json" 2>/dev/null
}
# The trap's last status becomes the script's: a cleanup that trips must not turn a PASS into a failure.
trap 'host_pids=$(stop_host); kill "$daemon" 2>/dev/null || true; wait "$daemon" 2>/dev/null || true; [[ -n "${host_pids// }" ]] && kill $host_pids 2>/dev/null; tmux -S "$state/tmux.sock" kill-server 2>/dev/null || true; rm -rf "$state" 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && break; sleep 1; done
# The link is a credential: it never reaches a file or the log.
link=$(env -i HOME="$state/home" PATH="$PATH" "$MEWLA" pair -details -state-dir "$state/mewla" "http://10.0.2.2:$PORT" 2>&1 | grep -o 'mewla://settings?p=[^ ]*' | head -1)
[[ -n "$link" ]] || fail "the daemon gave no pairing link"

log "1. the previous release pairs and keeps a draft"
adb uninstall "$PKG" > /dev/null 2>&1 || true
adb install "$OLD_APK" | tail -1 | tee -a "$OUT/upgrade.log"
old_code=$(version_code)
adb shell pm grant "$PKG" android.permission.POST_NOTIFICATIONS 2>/dev/null || true
adb shell am start -W -a android.intent.action.VIEW -d "'$link'" "$PKG" > /dev/null
unset link
tap "Pair and grant access" 60 || fail "the previous release did not ask to pair"
tap "Open Brain" 60 || fail "pairing did not reach Open Brain"
for _ in $(seq 1 60); do [[ "$(devices | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["devices"] or []))')" == 1 ]] && break; sleep 1; done
before=$(devices)
device_id=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["devices"][0]["id"])' "$before")
log "paired as device $device_id (versionCode $old_code)"
sleep 3
shot 1-paired
# Brain can still be settling when the composer first takes focus, and the typed text is lost: type it
# again until the composer shows it.
for attempt in 1 2 3; do
  tap "composer" 30 || fail "no composer to type the draft in"
  sleep 1
  adb shell input text "${DRAFT// /%s}"
  shows "$DRAFT" 5 && break
  log "the draft did not land in the composer (try $attempt)"
done
shows "$DRAFT" 5 || fail "the previous release's composer did not take the draft"
sleep 3 # the draft is written 400 ms after typing stops
shot 1-draft
adb shell am force-stop "$PKG"
sleep 2

log "2. this release installs over it"
install=$(adb install -r "$NEW_APK" 2>&1 | tail -1 || true)
log "install -r: $install"
[[ "$install" == Success* ]] || fail "install over the previous release refused (UPDATE_INCOMPATIBLE: another signing key; VERSION_DOWNGRADE: not a newer versionCode)"
new_code=$(version_code)
if [[ "${MEWLA_UPGRADE_SAME_VERSION_OK:-}" == 1 ]]; then
  # A build-only candidate carries main's tracked version, which can be the previous release's own.
  ((new_code >= old_code)) || fail "versionCode $old_code -> $new_code goes back"
else
  ((new_code > old_code)) || fail "versionCode $old_code -> $new_code is not an upgrade"
fi
log "versionCode $old_code -> $new_code"
seen_before=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["devices"][0].get("last_seen_at",""))' "$before")
sleep 2

log "3. it opens paired, Connected, with the same key and the draft"
adb shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 > /dev/null 2>&1
sleep 10
shot 3-opened
shows "Pair a computer" && fail "this release asks to pair again"
tap "home-cat" 30 || fail "home has no cat to open the computer page"
shows "re:^Connected" 30 || { shot 3-computer; fail "the computer page does not say Connected"; }
shot 3-computer
log "the computer page says Connected"
tap "page-back" 10 || adb shell input keyevent KEYCODE_BACK
shows "$DRAFT" 30 || { shot 3-no-draft; fail "Brain's composer lost the draft"; }
shot 3-draft
log "Brain's composer shows the draft"
after=$(devices)
python3 - "$before" "$after" "$seen_before" "$device_id" <<'PY' | tee -a "$OUT/upgrade.log"
import json, sys
before, after, seen_before, device_id = json.loads(sys.argv[1]), json.loads(sys.argv[2]), sys.argv[3], sys.argv[4]
ids = [d["id"] for d in after["devices"]]
assert ids == [device_id], f"devices after the upgrade: {ids}, expected only {device_id}"
seen_after = after["devices"][0].get("last_seen_at", "")
assert seen_after > seen_before, f"device not seen again after the upgrade ({seen_before} -> {seen_after})"
print(f"same device {device_id}, seen again at {seen_after}")
PY
adb root > /dev/null 2>&1 || true
sleep 2
storage=$(adb shell cat "/data/data/$PKG/files/nsh/$PKG/storage.json" 2>/dev/null || adb shell run-as "$PKG" cat "files/nsh/$PKG/storage.json")
echo "$storage" | python3 -I -c '
import json, sys
data = json.load(sys.stdin)
drafts = [v for k, v in data.items() if k.startswith("mewla.draft.v1:")]
assert any(sys.argv[1] in v for v in drafts), "the draft is not in storage"
servers = data["mewla:v3:servers"]
servers = json.loads(servers) if isinstance(servers, str) else servers
assert len(servers) == 1, f"{len(servers)} computers in storage, expected 1"
print("storage: the draft and the one computer came over")
' "$DRAFT" | tee -a "$OUT/upgrade.log"

# A release build can lose a code path to R8 (the M1 preview died on its first "Scan code"): open the
# scanner, answer the camera prompt, and the app must stay up with the camera looking for a code.
log "4. the scanner runs in this release build"
adb logcat -c
tap "home-cat" 20 || fail "home has no cat to open the computer page"
tap "Pair a computer" 20 || fail "the computer page has no Pair a computer"
tap "While using the app" 20 || fail "the scanner did not ask for the camera"
shows "qr-camera" 20 || true
sleep 3
shot 4-scanner
if [[ -z "$(adb shell pidof "$PKG" | tr -d '\r')" ]]; then
  adb logcat -d -b crash > "$OUT/scanner-crash.txt"
  fail "the app died with the scanner open (scanner-crash.txt)"
fi
shows "qr-camera" 5 || fail "the scanner is not looking for a code"
log "the scanner runs: camera allowed, app up, looking for a code"
log "PASS"
