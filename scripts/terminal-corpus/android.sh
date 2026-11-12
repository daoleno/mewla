#!/usr/bin/env bash
# The render corpus on an Android emulator: each screen replayed into the real TerminalView
# (debug builds' TerminalReplayActivity), one PNG per screen, cropped to the terminal.
#
#   android.sh <preview debug apk> <out dir> [name...]
#
# Boots its own headless emulator (AVD $AVD, default luma_reader_api35; read-only, no snapshot),
# inside flock /run/user/1000/mewla-emulator.lock, refuses while the Windows VM runs, and kills
# the emulator by PID at the end. ANDROID_SERIAL is the emulator's own. FONT=<dp> sets the text size
# (default: the view's own), LIGHT=1 the light theme.
set -euo pipefail
apk=$(readlink -f "${1:?apk}"); out=$(mkdir -p "${2:?out dir}" && cd "$2" && pwd); shift 2
here="$(cd "$(dirname "$0")" && pwd)"
avd=${AVD:-luma_reader_api35}
port=${EMU_PORT:-5582}
app=com.daoleno.mewla.preview.debug
if [[ -z "${MEWLA_EMULATOR_LOCKED:-}" ]]; then
  exec env MEWLA_EMULATOR_LOCKED=1 flock /run/user/1000/mewla-emulator.lock "$0" "$apk" "$out" "$@"
fi
if docker ps --format '{{.Names}}' | grep -qx mewla-win-1109; then echo "the Windows VM runs; not now" >&2; exit 1; fi
names=("$@")
[[ ${#names[@]} -gt 0 ]] || mapfile -t names < <(python3 -I -c "import json,sys;print('\n'.join(json.load(open(sys.argv[1]))))" "$here/corpus.json")

export ANDROID_SERIAL=emulator-$port
"$HOME/Android/Sdk/emulator/emulator" -avd "$avd" -port "$port" -no-window -no-audio -no-snapshot -read-only \
  -gpu swiftshader_indirect -memory 3072 -cores 4 > "$out/emulator.log" 2>&1 &
emu=$!
trap 'kill "$emu" 2>/dev/null; wait "$emu" 2>/dev/null || true' EXIT
adb wait-for-device
until [[ "$(adb shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == 1 ]]; do sleep 2; done
adb shell settings put global window_animation_scale 0
adb shell settings put global transition_animation_scale 0
adb install -r -t "$apk" > /dev/null
adb shell run-as "$app" mkdir -p files/corpus
for n in "${names[@]}"; do
  adb shell "run-as $app sh -c 'cat > files/corpus/$n.vt'" < "$here/$n.vt"
done
read -r cols rows < <(python3 -I -c "import json,sys;m=json.load(open(sys.argv[1]));print(m[sys.argv[2]]['cols'],m[sys.argv[2]]['rows'])" "$here/corpus.json" "${names[0]}")
for n in "${names[@]}"; do
  read -r cols rows < <(python3 -I -c "import json,sys;m=json.load(open(sys.argv[1]));print(m[sys.argv[2]]['cols'],m[sys.argv[2]]['rows'])" "$here/corpus.json" "$n")
  adb logcat -c
  adb shell am start -S -W -n "$app/com.daoleno.mewla.terminal.TerminalReplayActivity" \
    --es file "corpus/$n.vt" --ei cols "$cols" --ei rows "$rows" ${LIGHT:+--ez light true} ${FONT:+--ef font "$FONT"} > /dev/null
  rect=""
  for _ in $(seq 1 40); do
    rect=$(adb logcat -d -s TerminalReplay:I | sed -n 's/.*rect \([0-9]*\) \([0-9]*\) \([0-9]*\) \([0-9]*\).*/\3x\4+\1+\2/p' | tail -1)
    [[ -n "$rect" ]] && break
    sleep 0.5
  done
  [[ -n "$rect" ]] || { echo "$n: no rect"; adb logcat -d | tail -20 >&2; continue; }
  sleep 0.5
  adb exec-out screencap -p > "$out/full-$n.png"
  magick "$out/full-$n.png" -crop "$rect" +repage "$out/$n.png" && rm "$out/full-$n.png"
  echo "$n $rect"
done
adb logcat -d -s TerminalReplay:I | grep -m1 'cell' || true
