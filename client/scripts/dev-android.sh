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
# The start link, the one way in: tap Load on the device to confirm.
adb shell am start -a android.intent.action.VIEW -d "'mewla-dev://start?server=ws://127.0.0.1:$port/ws'"
echo "Tap Load on the device."
wait "$server"
