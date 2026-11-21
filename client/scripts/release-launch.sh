#!/usr/bin/env bash
# The release's launch smoke on an Android emulator that is already booted (adb sees one device): the
# exact signed APK this run built installs on a fresh device, reports the release's version, opens,
# and is still running with nothing in the crash log after ten seconds; then, on the first-run screen, its scanner opens
# in this release build (R8 once dropped ML Kit's registrar and the first "Scan code" killed the app): the system's
# camera prompt, then the camera looking for a code (the finder holds `qr-camera` only
# while it runs) with the app up and nothing in the crash log.
# release-artifacts.yml runs it before publishing, on an emulator that boots while the APK builds.
# Inputs: VERSION (X.Y.Z), OUT (evidence directory), and APK (the signed APK), or ARTIFACT (this run's
# artifact holding it, waited for up to 40 min; GH_TOKEN, GITHUB_REPOSITORY, GITHUB_RUN_ID) with
# BUILD_JOB (the job that uploads it, so its failure ends the wait). Exit 0 only if all of it holds.
set -euo pipefail
: "${VERSION:?}" "${OUT:?}"
PKG="${PKG:-ai.mewla.app}"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$OUT"
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/launch.log"; }
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
  dir="$(mktemp -d)"
  fetch_artifact "$ARTIFACT" "$BUILD_JOB" "$dir"
  APK="$(find "$dir" -name '*.apk' -print -quit)"
  [[ -n "$APK" ]] || fail "$ARTIFACT holds no APK"
  log "downloaded $(basename "$APK")"
fi

adb uninstall "$PKG" > /dev/null 2>&1 || true
log "install: $(adb install "$APK" 2>&1 | tail -1)"
installed="$(adb shell dumpsys package "$PKG" | grep -m1 -o 'versionName=[^ ]*' | cut -d= -f2 | tr -d '\r')"
[[ "$installed" == "$VERSION" ]] || fail "installed versionName is '$installed', want $VERSION"
log "versionName $installed"

adb logcat -c
adb shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 > /dev/null 2>&1
sleep 10
pid="$(adb shell pidof "$PKG" | tr -d '\r')"
[[ -n "$pid" ]] || fail "the app is not running ten seconds after launch"
crashes="$(adb logcat -d -b crash 2>/dev/null | grep -c "$PKG" || true)"
[[ "$crashes" == 0 ]] || fail "the crash log names $PKG"
adb exec-out screencap -p > "$OUT/launched.png" 2>/dev/null || true
log "running as pid $pid, no crash: launched"

# The first run: Welcome offers the scanner ("Mewla already running? Scan its code"). Notifications are granted beforehand so
# that their prompt is not in the way of the camera's.
adb shell pm grant "$PKG" android.permission.POST_NOTIFICATIONS 2>/dev/null || true
adb logcat -c
tap "scan-now" 30 || { adb exec-out screencap -p > "$OUT/no-scan-now.png" 2>/dev/null || true; fail "the first-run screen has no way to the scanner (scan-now)"; }
# The scan step asks for the camera as it opens; the app's own "Allow camera" shows only when it has not asked yet.
if shows "allow-camera" 2; then tap "allow-camera" 5; fi
tap "While using the app" 20 || fail "the system did not ask for the camera"
shows "qr-camera" 20 || fail "the scanner is not looking for a code (no qr-camera)"
sleep 3
adb exec-out screencap -p > "$OUT/scanner.png" 2>/dev/null || true
[[ -n "$(adb shell pidof "$PKG" | tr -d '\r')" ]] || fail "the app died with the scanner open"
crashes="$(adb logcat -d -b crash 2>/dev/null | grep -c "$PKG" || true)"
[[ "$crashes" == 0 ]] || fail "the crash log names $PKG after the scanner opened"
shows "qr-camera" 5 || fail "the scanner stopped looking for a code"
log "the scanner runs: camera allowed, app up, looking for a code"
log "PASS"
