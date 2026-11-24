#!/usr/bin/env bash
# "+" with the keyboard up: the attach sheet; a photo from the system photo picker, a shot from the camera (the system's
# prompt first when the camera is not granted yet), a file from Files; three chips; one removed; Send carries the two.
# Needs a picture in the phone's gallery and trip-plan.pdf in Download (pushed below), and the Dev app with
# files.capture (a build from 0.4.27 on). SHOTS=DIR keeps a screenshot of each step there.
set -euo pipefail
dir="$(mktemp -d "${TMPDIR:-/tmp}/attach-sheet.XXXXXX")"
python3 -I -c 'import base64,sys;open(sys.argv[1],"wb").write(base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="))' "$dir/Screenshot_lane.png"
printf '%%PDF-1.4\n%%%%EOF\n' > "$dir/trip-plan.pdf"
adb shell mkdir -p /sdcard/Pictures/Screenshots /sdcard/Download
adb push -q "$dir/Screenshot_lane.png" /sdcard/Pictures/Screenshots/Screenshot_lane.png
adb push -q "$dir/trip-plan.pdf" /sdcard/Download/trip-plan.pdf
adb shell am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d file:///sdcard/Pictures/Screenshots/Screenshot_lane.png >/dev/null
adb shell am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d file:///sdcard/Download/trip-plan.pdf >/dev/null
# A failed run leaves its words and chips in the composer, and the next reset waits for an empty one: clear them.
cleanup() {
  [[ $? -eq 0 ]] && return
  adb shell input keyevent KEYCODE_BACK
  for _ in 1 2 3; do "$PHONE" tap '^Remove ' 2 >/dev/null 2>&1 || break; done
  "$PHONE" tap '^composer$' 5 >/dev/null 2>&1 || return 0
  adb shell input keyevent KEYCODE_MOVE_END
  for _ in $(seq 1 16); do adb shell input keyevent KEYCODE_DEL; done
  adb shell input keyevent KEYCODE_BACK
}
trap cleanup EXIT
shot() { if [[ -n "${SHOTS:-}" ]]; then "$PHONE" shot "$SHOTS/$1.png" >/dev/null; fi; }
# A wait that fails says which, with what the screen held: under `set -e` a bare `shows` ended the flow with an
# empty log (0.4.28 gate, API 35, the run right after a reset).
need() {
  "$PHONE" shows "$1" "${2:-10}" && return 0
  echo "attach-sheet: '$1' not shown within ${2:-10} s" >&2
  "$PHONE" dump >&2 || true
  shot "failed-$(printf '%s' "$1" | tr -c 'A-Za-z0-9' '-')"
  return 1
}
chips() { "$PHONE" dump | grep -c '^attachment-upload-[0-9]* | .*, Ready$' || true; }
until_chips() { for _ in $(seq 1 40); do [[ "$(chips)" == "$1" ]] && return 0; sleep 0.5; done; echo "expected $1 ready chips, saw $(chips)" >&2; return 1; }

msg="attach$RANDOM"
"$PHONE" type "$msg"
shot 1-keyboard
"$PHONE" tap '^Attach$'
# Right after a reset the first tap can land while the keyboard still moves the composer: tap again, once.
if ! "$PHONE" shows 'attach-photos' 5; then "$PHONE" tap '^Attach$'; need 'attach-photos' 10; fi
shot 2-sheet
"$PHONE" tap '^Photos$'
need 'Photo taken on' 15
shot 3-photo-picker
"$PHONE" tap 'Photo taken on'
# The photo picker's confirm: "Add (1)" up to Android 15, "Done" on Android 16.
"$PHONE" tap '^(Add \(1\)|Done)$'
until_chips 1
shot 4-photo

"$PHONE" tap '^Attach$'
"$PHONE" tap '^Camera$'
if "$PHONE" shows 'While using the app' 4; then shot 5-camera-prompt; "$PHONE" tap '^While using the app$'; fi
need 'Shutter' 15
"$PHONE" tap '^Shutter$'
need 'done_button' 15
shot 6-camera
"$PHONE" tap '^Done$'
until_chips 2

"$PHONE" tap '^Attach$'
"$PHONE" tap '^Files$'
need 'trip-plan.pdf' 15
shot 7-files
"$PHONE" tap '^trip-plan.pdf$'
until_chips 3
shot 8-three

"$PHONE" tap '^Remove IMG_'
until_chips 2
shot 9-removed
"$PHONE" tap '^send$'
need "$msg" 15
! "$PHONE" shows 'attachment-upload-' 2
shot 10-sent
