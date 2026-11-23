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
shot() { if [[ -n "${SHOTS:-}" ]]; then "$PHONE" shot "$SHOTS/$1.png" >/dev/null; fi; }
chips() { "$PHONE" dump | grep -c '^attachment-upload-[0-9]* | .*, Ready$' || true; }
until_chips() { for _ in $(seq 1 40); do [[ "$(chips)" == "$1" ]] && return 0; sleep 0.5; done; echo "expected $1 ready chips, saw $(chips)" >&2; return 1; }

msg="attach$RANDOM"
"$PHONE" type "$msg"
shot 1-keyboard
"$PHONE" tap '^Attach$'
"$PHONE" shows 'attach-photos' 5
shot 2-sheet
"$PHONE" tap '^Photos$'
"$PHONE" shows 'Photo taken on' 15
shot 3-photo-picker
"$PHONE" tap 'Photo taken on'
"$PHONE" tap '^Add \(1\)$'
until_chips 1
shot 4-photo

"$PHONE" tap '^Attach$'
"$PHONE" tap '^Camera$'
if "$PHONE" shows 'While using the app' 4; then shot 5-camera-prompt; "$PHONE" tap '^While using the app$'; fi
"$PHONE" shows 'Shutter' 15
"$PHONE" tap '^Shutter$'
"$PHONE" shows 'done_button' 15
shot 6-camera
"$PHONE" tap '^Done$'
until_chips 2

"$PHONE" tap '^Attach$'
"$PHONE" tap '^Files$'
"$PHONE" shows 'trip-plan.pdf' 15
shot 7-files
"$PHONE" tap '^trip-plan.pdf$'
until_chips 3
shot 8-three

"$PHONE" tap '^Remove IMG_'
until_chips 2
shot 9-removed
"$PHONE" tap '^send$'
"$PHONE" shows "$msg" 15
! "$PHONE" shows 'attachment-upload-' 2
shot 10-sent
