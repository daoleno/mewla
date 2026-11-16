#!/usr/bin/env bash
# The release's launch smoke on an Android emulator that is already booted (adb sees one device): the
# exact signed APK this run built installs on a fresh device, reports the release's version, opens,
# and is still running with nothing in the crash log after ten seconds.
# release-artifacts.yml runs it before publishing, on an emulator that boots while the APK builds.
# Inputs: VERSION (X.Y.Z), OUT (evidence directory), and APK (the signed APK), or ARTIFACT (this run's
# artifact holding it, waited for up to 40 min; GH_TOKEN, GITHUB_REPOSITORY, GITHUB_RUN_ID) with
# BUILD_JOB (the job that uploads it, so its failure ends the wait). Exit 0 only if all of it holds.
set -euo pipefail
: "${VERSION:?}" "${OUT:?}"
PKG=com.daoleno.mewla
mkdir -p "$OUT"
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/launch.log"; }
fail() {
  log "FAIL: $*"
  adb exec-out screencap -p > "$OUT/fail.png" 2>/dev/null || true
  adb logcat -d -b crash > "$OUT/crash.txt" 2>/dev/null || true
  exit 1
}

if [[ -z "${APK:-}" ]]; then
  : "${ARTIFACT:?}" "${BUILD_JOB:?}"
  # The artifacts API, not `gh run download`: this run is still going.
  api="repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID"
  id=""
  log "waiting for $ARTIFACT"
  for _ in $(seq 1 240); do
    id="$(gh api "$api/artifacts?name=$ARTIFACT" --jq '.artifacts[0].id // empty' 2>&1)" || { log "artifacts API: $id"; id=""; }
    [[ -z "$id" ]] || break
    state="$(gh api "$api/jobs?per_page=100" --jq ".jobs[] | select(.name == \"$BUILD_JOB\") | \"\(.status) \(.conclusion)\"" 2>&1)" || state=""
    [[ "$state" != completed* || "$state" == "completed success" ]] || fail "$BUILD_JOB ended without an APK ($state)"
    sleep 10
  done
  [[ -n "$id" ]] || fail "no $ARTIFACT after 40 min"
  dir="$(mktemp -d)"
  timeout 600 gh api "repos/$GITHUB_REPOSITORY/actions/artifacts/$id/zip" > "$dir/apk.zip" || fail "downloading artifact $id failed"
  (cd "$dir" && unzip -q apk.zip) || fail "artifact $id is not a zip"
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
