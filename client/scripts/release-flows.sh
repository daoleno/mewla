#!/usr/bin/env bash
# The release's journeys on the exact signed APK, on an Android emulator that is already booted (adb sees one device): the
# flow files of the local phone lane (flows/*.sh: send a message, open Calendar, share a picture in) replayed against the
# release build, paired to a test daemon of its own.
#   1. a daemon of its own (the release's own Linux binary), loopback, own state, an inert Brain host;
#   2. the APK installs on a fresh device and pairs by its link (Pair and grant access, Open Brain);
#   3. `phone.sh flow all` with PHONE_EXTERNAL=1: each flow after the app is stopped and started again, PASS or FAIL per flow.
# The first-run walk and the scanned QR stay a person's, once per release; release-launch.sh opens the scanner.
# release-artifacts.yml runs it after release-launch.sh on the same emulator. Inputs: OUT (evidence directory); MEWLA (the daemon
# binary) or DAEMON_ARTIFACT (this run's artifact holding mewla-linux-amd64) with DAEMON_JOB; APK, or ARTIFACT with BUILD_JOB
# (see release-launch.sh; GH_TOKEN, GITHUB_REPOSITORY, GITHUB_RUN_ID); PKG (default com.daoleno.mewla), PORT (9943).
set -euo pipefail
: "${OUT:?}"
PKG="${PKG:-com.daoleno.mewla}"
PORT="${PORT:-9943}"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$OUT"
state="$(mktemp -d)"
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/flows.log"; }
fail() {
  log "FAIL: $*"
  adb exec-out screencap -p > "$OUT/fail.png" 2>/dev/null || true
  adb logcat -d -b crash > "$OUT/crash.txt" 2>/dev/null || true
  exit 1
}
# shellcheck source=lib/adbui.sh
source "$here/lib/adbui.sh"
# shellcheck source=lib/artifact.sh
source "$here/lib/artifact.sh"

if [[ -z "${APK:-}" ]]; then
  : "${ARTIFACT:?}" "${BUILD_JOB:?}"
  fetch_artifact "$ARTIFACT" "$BUILD_JOB" "$state/apk"
  APK="$(find "$state/apk" -name '*.apk' -print -quit)"
  [[ -n "$APK" ]] || fail "$ARTIFACT holds no APK"
fi
if [[ -z "${MEWLA:-}" ]]; then
  : "${DAEMON_ARTIFACT:?}" "${DAEMON_JOB:?}"
  fetch_artifact "$DAEMON_ARTIFACT" "$DAEMON_JOB" "$state/daemon"
  MEWLA="$(find "$state/daemon" -name mewla-linux-amd64 -print -quit)"
  [[ -n "$MEWLA" ]] || fail "$DAEMON_ARTIFACT holds no mewla-linux-amd64"
  chmod +x "$MEWLA"
fi

mkdir -p "$state/home" "$state/bin"
# A daemon of its own: own state, nothing from this shell, an inert Brain host (a cursor-agent that answers nothing, so Brain
# starts and its composer shows) and no systemd-run (the session host is then its own child, never a user unit).
printf '#!/bin/sh\n[ "$1" = --version ] && { echo 0.0.0-flows; exit 0; }\nexec sleep 86400\n' > "$state/bin/cursor-agent"
printf '#!/bin/sh\necho "systemd-run is off for the release flows daemon" >&2\nexit 1\n' > "$state/bin/systemd-run"
chmod +x "$state/bin/cursor-agent" "$state/bin/systemd-run"
env -i HOME="$state/home" PATH="$state/bin:$PATH" TERM=xterm-256color MEWLA_BRAIN_HOST_EXECUTOR=agent MEWLA_NO_AUTOUPDATE=1 \
  "$MEWLA" serve -addr "127.0.0.1:$PORT" -loopback-only -state-dir "$state/mewla" > "$OUT/daemon.log" 2>&1 &
daemon=$!
stop_host() {
  env -i HOME="$state/home" PATH="$PATH" "$MEWLA" host status --state-dir "$state/mewla" --json > "$state/host.json" 2>/dev/null || return 0
  python3 -I -c 'import json, sys
d = json.load(open(sys.argv[1]))
print(" ".join(str(s["pid"]) for s in d.get("sessions") or []), (d.get("host") or {}).get("pid", ""))' "$state/host.json" 2>/dev/null
}
trap 'host_pids=$(stop_host); kill "$daemon" 2>/dev/null || true; wait "$daemon" 2>/dev/null || true; [[ -n "${host_pids// }" ]] && kill $host_pids 2>/dev/null; rm -rf "$state" 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do (exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && break; sleep 1; done
# The link is a credential: it never reaches a file or the log.
link=$(env -i HOME="$state/home" PATH="$PATH" "$MEWLA" pair -details -state-dir "$state/mewla" "http://10.0.2.2:$PORT" 2>&1 | grep -o 'mewla://settings?p=[^ ]*' | head -1)
[[ -n "$link" ]] || fail "the daemon gave no pairing link"

net_wait=$SECONDS
until adb shell dumpsys connectivity 2>/dev/null | grep -q 'VALIDATED' && adb shell ping -c 1 -W 2 10.0.2.2 > /dev/null 2>&1; do
  ((SECONDS - net_wait < 120)) || fail "the emulator had no validated network after 120 s"
  sleep 1
done

log "1. the release build installs and pairs"
adb uninstall "$PKG" > /dev/null 2>&1 || true
log "install: $(adb install "$APK" 2>&1 | tail -1)"
adb shell pm grant "$PKG" android.permission.POST_NOTIFICATIONS 2>/dev/null || true
adb shell am start -W -a android.intent.action.VIEW -d "'$link'" "$PKG" > /dev/null
unset link
tap "Pair and grant access" 60 || fail "the app did not ask to pair"
tap "Open Brain" 60 || fail "pairing did not reach Open Brain"
log "paired"

log "2. the flows"
adb unroot > /dev/null 2>&1 || true # a share started by a root adb is refused
failed=0
PHONE_EXTERNAL=1 PHONE_PKG="$PKG" MEWLA_LANE_HOME="$OUT/lane" "$here/phone.sh" flow all 2>&1 | tee -a "$OUT/flows.log" || failed=1
for f in "$OUT"/lane/flow-*.log; do [[ -f "$f" ]] && cp "$f" "$OUT/"; done
[[ "$failed" == 0 ]] || fail "a flow failed (flow-*.log)"
log "PASS"
