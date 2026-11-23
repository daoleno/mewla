#!/usr/bin/env bash
# mac.sh <Mewla Dev zip>: on a CI Mac (macos-app.yml `relay-lan`), Mewla Dev on a computer reached through Mewla
# Relay: relay, then the LAN, then the LAN cut (pf), relay, then the LAN again, with the path the app shows and a
# reminder sent on each path that reaches the computer exactly once. Owner 11-23: did the desktop app ever find the LAN?
# Two isolated daemons: the computer (home1, signed in to a relay of its own, up.sh) and Mewla Dev's own.
set -euo pipefail
zip="$1"; here="$(cd "$(dirname "$0")" && pwd)"; root="$(cd "$here/../../.." && pwd)"
E="$RUNNER_TEMP/relay-lan"; mkdir -p "$E/bin" "$E/captures"; D="$E/dev"; mkdir -p "$D/data"
ditto -x -k "$zip" "$D"
cp "$D/Mewla Dev.app/Contents/Helpers/mewla" "$E/bin/mewla"
(cd "$root/daemon" && go build -o "$E/bin/mewla-account" ./cmd/mewla-account && go build -o "$E/bin/mewla-relay" ./cmd/mewla-relay)
GOBIN="$E/bin" go install github.com/letsencrypt/pebble/v2/cmd/pebble@v2.6.0 github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@v2.6.0
# Every *.mewla.test name is this Mac: challtestsrv answers it with 127.0.0.1.
sudo mkdir -p /etc/resolver && printf 'nameserver 127.0.0.1\nport 8053\n' | sudo tee /etc/resolver/mewla.test >/dev/null
"$here/up.sh" "$E"
"$here/computer.sh" "$E" home1 19876 "$E/bin/mewla"
lan=$(ipconfig getifaddr en0)
link=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["phone_link"])' "$E/home1-pair.json")
echo "the computer's LAN address: $lan"

printf '%s' '{"mewla:theme_preference":"classic-light"}' > "$D/data/storage.json"
env -i HOME="$HOME" USER="$USER" LOGNAME="$USER" SHELL=/bin/bash LANG=en_US.UTF-8 TMPDIR="$TMPDIR" PATH=/usr/bin:/bin:/usr/sbin:/sbin \
  MEWLA_APP_STATE_ROOT="$D/state" MEWLA_APP_DATA_DIR="$D/data" MEWLA_APP_ADDR=127.0.0.1:19877 \
  MEWLA_APP_PHONE_ADDRESS=http://127.0.0.1:19877 MEWLA_APP_DEV_SCRIPT="{\"relay\":\"$link\"}" \
  "$D/Mewla Dev.app/Contents/MacOS/Mewla Dev" > "$D/app.out" 2>&1 &
echo $! > "$E/pids/app"
log() { cat "$D"/data/logs/* 2>/dev/null | grep "dev script: relay" || true; }
paths() { log | grep -c "dev script: relay path $1" || true; }
capture() { "$root/client/macos/scripts/capture-windows.sh" "Mewla Dev" "$E/captures/$1" "$1-" >/dev/null 2>&1 || true; }
wait_path() { # wait_path <label> <count> <seconds>
  for i in $(seq 1 "$3"); do [ "$(paths "$1")" -ge "$2" ] && { echo "on $1 (#$2) after $i s"; return 0; }; sleep 1; done
  echo "never on $1 (#$2)"; log; return 1
}
cut() { printf 'block return quick proto tcp from any to %s port 19876\nblock return quick proto tcp from %s port 19876 to any\n' "$lan" "$lan" \
  | sudo pfctl -a com.apple/250.MewlaRelayLan -f - 2>/dev/null; sudo pfctl -E >/dev/null 2>&1 || true; sudo pfctl -k "$lan" >/dev/null 2>&1 || true; }
restore() { sudo pfctl -a com.apple/250.MewlaRelayLan -F all >/dev/null 2>&1 || true; }
trap 'restore; kill "$(cat "$E/pids/app")" 2>/dev/null || true' EXIT

ok=0
wait_path Relayed 1 180 && sleep 3 && capture 1-relay || ok=1
[ $ok = 0 ] && wait_path LAN 1 240 && sleep 3 && capture 2-lan || ok=1
if [ $ok = 0 ]; then
  echo "cutting $lan:19876 (pf)"; cut
  wait_path Relayed 2 120 && sleep 3 && capture 3-cut || ok=1
  restore; echo "restored $lan:19876"
fi
[ $ok = 0 ] && wait_path LAN 2 300 && sleep 3 && capture 4-lan-again || ok=1
sleep 8
echo "--- what the app showed and sent"; log | sed -E 's/.*(dev script: relay)/\1/'
db="$E/home1/.mewla/calendar/calendar.db"
echo "--- reminders on the computer"
sqlite3 "$db" "select json_extract(body,'\$.title'), count(*) from cal_item where json_extract(body,'\$.title') like 'relay proof on %' group by 1" | tee "$E/reminders.txt"
created=$(log | grep -c 'reminder ".*" created' || true)
[ "$(wc -l < "$E/reminders.txt" | tr -d ' ')" -ge 4 ] && [ "$(wc -l < "$E/reminders.txt" | tr -d ' ')" = "$created" ] || { echo "reminders: $created created, on the computer $(wc -l < "$E/reminders.txt")"; ok=1; }
grep -qv '|1$' "$E/reminders.txt" && { echo "a reminder is on the computer more than once"; ok=1; }
if [ $ok != 0 ]; then
  for f in "$E"/logs/home1.log "$E"/logs/relay.log "$D"/data/logs/*; do echo "--- $f"; tail -40 "$f" 2>/dev/null | cut -c1-300; done
fi
exit $ok
