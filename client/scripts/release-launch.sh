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
  for _ in $(seq 1 240); do
    [[ -n "$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/artifacts?name=$ARTIFACT" --jq '.artifacts[0].id // empty')" ]] && break
    state="$(gh api "repos/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID/jobs?per_page=100" \
      --jq ".jobs[] | select(.name == \"$BUILD_JOB\") | \"\(.status) \(.conclusion)\"")"
    [[ "$state" != completed* || "$state" == "completed success" ]] || { echo "error: $BUILD_JOB ended without an APK ($state)" >&2; exit 1; }
    sleep 10
  done
  dir="$(mktemp -d)"
  timeout 600 gh run download "$GITHUB_RUN_ID" --repo "$GITHUB_REPOSITORY" -n "$ARTIFACT" -D "$dir"
  APK="$(find "$dir" -name '*.apk' -print -quit)"
  [[ -n "$APK" ]] || { echo "error: $ARTIFACT holds no APK after 40 min" >&2; exit 1; }
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
