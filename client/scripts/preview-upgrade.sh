#!/usr/bin/env bash
# The Preview's in-place upgrade on an Android emulator that is already booted (adb sees one device):
# the previous dispatched Preview (PREV_APK, client.yml's last successful run on this branch) is
# installed, then this run's Preview (PREVIEW_APK) is installed over it with `install -r`.
# Both must carry Mewla's release key (preview-signer.sh). A previous Preview signed by a runner's
# debug key (built before 11-12) cannot be upgraded by anything: that run is logged as the
# transition and only this Preview's own signer is held to the key.
# Inputs: PREVIEW_APK, OUT (evidence directory); PREV_APK may be empty or missing (none kept).
set -euo pipefail
: "${PREVIEW_APK:?}" "${OUT:?}"
PKG=com.daoleno.mewla.preview
HERE="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$OUT"
log() { echo "[$(date +%T)] $*" | tee -a "$OUT/preview.log"; }
fail() { log "FAIL: $*"; exit 1; }

mine="$("$HERE/preview-signer.sh" "$PREVIEW_APK")" || fail "this Preview is signed by $mine, not the release key"
log "this Preview's signer: $mine"
adb uninstall "$PKG" >/dev/null 2>&1 || true
if [[ -z "${PREV_APK:-}" || ! -f "${PREV_APK:-}" ]]; then
  log "no previous Preview kept for this branch: only its signer is checked"
elif ! prev="$("$HERE/preview-signer.sh" "$PREV_APK" 2>/dev/null)"; then
  log "transition: the previous Preview is signed by $prev (a runner's debug key); installed Previews from before need one uninstall"
else
  log "previous Preview's signer: $prev"
  out="$(adb install "$PREV_APK" 2>&1 | tail -1)"
  [[ "$out" == Success* ]] || fail "the previous Preview did not install: $out"
  out="$(adb install -r "$PREVIEW_APK" 2>&1 | tail -1)"
  log "install -r over the previous Preview: $out"
  [[ "$out" == Success* ]] || fail "this Preview does not install over the previous one (INSTALL_FAILED_UPDATE_INCOMPATIBLE means another signing key)"
fi
adb uninstall "$PKG" >/dev/null 2>&1 || true
log "PASS"
