#!/usr/bin/env bash
# Hot reload on an Android phone or emulator: builds and installs the Dev app (preview debug) given this
# host's `nsh dev` server, then serves client/ from it. Open Mewla Dev from its icon and tap Load once:
# every save in client/ reloads it, on USB or over Wi-Fi. One device (adb devices).
# DEV_HOST overrides the address the phone reaches this host at (default: its LAN address).
set -euo pipefail
client="$(cd "$(dirname "$0")/.." && pwd)"
port="${PORT:-8790}"
host="${DEV_HOST:-}"
if [ -z "$host" ]; then
  if command -v ip >/dev/null; then
    host="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "src") print $(i + 1)}')"
  else
    host="$(ipconfig getifaddr en0 2>/dev/null || true)"
  fi
fi
[ -n "$host" ] || { echo "No LAN address found; set DEV_HOST." >&2; exit 1; }
(cd "$client/android" && ./gradlew -q installPreviewDebug "-Pmewla.devServer=ws://$host:$port/ws")
nsh dev --server-only --port "$port" "$client/ui" &
server=$!
trap 'kill "$server" 2>/dev/null || true' EXIT
until (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; do
  kill -0 "$server" || exit 1
  sleep 0.2
done
adb shell monkey -p com.daoleno.mewla.preview.debug -c android.intent.category.LAUNCHER 1 >/dev/null
echo "Mewla Dev opens on ws://$host:$port/ws: tap Load on the device the first time."
wait "$server"
