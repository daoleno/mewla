#!/usr/bin/env bash
# The runner's side of mac-loop.sh: remote.sh ui|host|daemon|launch, in the synced checkout. Prints a time per stage.
set -euo pipefail
source ~/mac-loop/env.sh
ws="$(cat ~/mac-loop/workspace)"; cd "$ws"
L="$HOME/mac-loop"; app="$L/app/Mewla Dev.app"; D="$L/run"
now() { python3 -c 'import time; print(time.time())'; }
stamp() { python3 -c 'import sys,time; print("  runner %-20s %6.1f s" % (sys.argv[1], time.time()-float(sys.argv[2])))' "$1" "$2"; }

build_ui() {
  local t; t=$(now)
  # As package.sh: a copy of client/core and client/ui at the Mac's type sizes, `nsh build --release`.
  local w="$L/uibuild"; mkdir -p "$w/src"
  rsync -a --delete --exclude .nsh --exclude node_modules client/core client/ui "$w/src/"
  python3 -I - "$w/src/ui/nsh.json" client/macos/density.json <<'PY'
import json, sys
path, density = sys.argv[1:3]
c = json.load(open(path)); c.setdefault("tailwind", {})["fontSize"] = json.load(open(density))["fontSize"]
json.dump(c, open(path, "w"), indent=2)
PY
  rm -rf "$w/nsh"
  ~/.nsh/bin/nsh build --release -o "$w/nsh" "$w/src/ui" >/dev/null
  rm -f "$w/nsh/bundle.js.map"
  cp "$app/Contents/Resources/nsh/app.json" "$w/nsh/app.json"
  rsync -a --delete "$w/nsh/" "$app/Contents/Resources/nsh/"
  stamp "nsh build" "$t"
}
build_host() {
  local t; t=$(now)
  # The loop profile (client/macos/Cargo.toml [profile.loop]); the workflow may have started its cold build already.
  if [[ -f "$L/prewarm.pid" ]] && kill -0 "$(cat "$L/prewarm.pid")" 2>/dev/null; then
    echo "  waiting for the loop profile's first build (started by the workflow)"
    while kill -0 "$(cat "$L/prewarm.pid")" 2>/dev/null; do sleep 2; done
    stamp "waited for prewarm" "$t"
  fi
  cargo build --profile loop --manifest-path client/macos/Cargo.toml 2>&1 | tail -2
  cp client/macos/target/loop/Mewla "$app/Contents/MacOS/Mewla Dev"
  stamp "cargo build" "$t"
}
build_daemon() {
  local t; t=$(now)
  export MEWLA_GHOSTTY_DESKTOP_CACHE="$HOME/.cache/mewla/ghostty-release/darwin_arm64" MEWLA_BUILD_TMPDIR="$RUNNER_TEMP/mewla-build"
  ./scripts/build-daemon-linux.sh --out-dir "$RUNNER_TEMP/daemon" --targets darwin-arm64 --web-ui prebuilt >/dev/null
  cp "$RUNNER_TEMP/daemon/mewla-darwin-arm64" "$app/Contents/Helpers/mewla"
  stamp "go build" "$t"
}
relaunch() {
  local t; t=$(now)
  if [[ -f "$D/app.pid" ]] && kill -0 "$(cat "$D/app.pid")" 2>/dev/null; then
    kill -TERM "$(cat "$D/app.pid")"
    for i in $(seq 1 120); do pgrep -f "$L/app/" >/dev/null || break; sleep 0.25; done
    pkill -TERM -f "$L/app/" || true
  fi
  codesign --force --deep -s - "$app" 2>/dev/null
  mkdir -p "$D/data"
  [[ -f "$D/data/storage.json" ]] || printf '%s' '{"mewla:theme_preference":"classic-light"}' > "$D/data/storage.json"
  local seen; seen=$(cat "$D"/data/logs/* 2>/dev/null | grep -c "dev script: connected" || true)
  env -i HOME="$HOME" USER="$USER" LOGNAME="$USER" SHELL=/bin/bash LANG=en_US.UTF-8 TMPDIR="$TMPDIR" PATH=/usr/bin:/bin:/usr/sbin:/sbin \
    MEWLA_APP_STATE_ROOT="$D/state" MEWLA_APP_DATA_DIR="$D/data" MEWLA_APP_ADDR=127.0.0.1:19877 \
    MEWLA_APP_PHONE_ADDRESS=http://127.0.0.1:19877 MEWLA_APP_WINDOW_SIZE=1280x800 MEWLA_APP_DEV_SCRIPT='{}' \
    nohup "$app/Contents/MacOS/Mewla Dev" > "$D/app.out" 2>&1 &
  echo $! > "$D/app.pid"
  for i in $(seq 1 480); do
    [[ "$(cat "$D"/data/logs/* 2>/dev/null | grep -c "dev script: connected" || true)" -gt "${seen:-0}" ]] && break
    sleep 0.25
  done
  stamp "relaunch to connected" "$t"
  osascript -e 'tell application "System Events" to set frontmost of process "Mewla Dev" to true' >/dev/null 2>&1 || true
  sleep "${MAC_LOOP_SETTLE:-2}"
}

case "${1:-}" in
ui) build_ui; relaunch ;;
host) build_host; relaunch ;;
daemon) build_daemon; relaunch ;;
launch) relaunch ;;
*) echo "remote.sh ui|host|daemon|launch" >&2; exit 2 ;;
esac
