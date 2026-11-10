#!/usr/bin/env bash
# Hot reload on an Android phone or emulator: installs the preview debug build and runs the app
# from `nsh dev`, so every save in client/ reloads it. One device (adb devices); USB is enough.
set -euo pipefail
client="$(cd "$(dirname "$0")/.." && pwd)"
port="${PORT:-8790}"
(cd "$client/android" && ./gradlew -q installPreviewDebug)
nsh dev --server-only --port "$port" "$client/ui" &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT
until (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
  kill -0 "$server" || exit 1
  sleep 0.2
done
adb reverse "tcp:$port" "tcp:$port"
adb shell am start -S -n com.daoleno.mewla.preview.debug/com.daoleno.mewla.MainActivity --es devServer "ws://127.0.0.1:$port/ws"
wait "$server"
