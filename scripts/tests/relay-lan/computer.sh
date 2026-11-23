#!/usr/bin/env bash
# computer.sh E name port bin: an isolated computer (own HOME, state dir and port) signed in to up.sh's account
# service with remote access on, and its pairing as `mewla pair -json` prints it, in $E/<name>-pair.json.
set -euo pipefail
E="$1"; n="$2"; port="$3"; bin="$4"; H="$E/$n"; mkdir -p "$H/.mewla"; touch "$H/.zshrc"
run=(env -i HOME="$H" PATH="$E/stubs:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin" TERM=xterm-256color MEWLA_NO_AUTOUPDATE=1
  MEWLA_BRAIN_HOST_EXECUTOR=agent MEWLA_REMOTE_SERVICE_URL=http://127.0.0.1:18090)
bg() { local name=$1; shift; if command -v setsid >/dev/null; then setsid "$@" > "$E/logs/$name.log" 2>&1 < /dev/null & else nohup "$@" > "$E/logs/$name.log" 2>&1 < /dev/null & fi; echo $! > "$E/pids/$name"; }
cd "$H"
bg "$n" "${run[@]}" "$bin" serve -addr "127.0.0.1:$port" -state-dir "$H/.mewla"
for i in $(seq 1 30); do curl -sf -o /dev/null "http://127.0.0.1:$port/health" && break; sleep 1; done
bg "$n-login" "${run[@]}" "$bin" login -state-dir "$H/.mewla"
for i in $(seq 1 30); do code=$(grep -o 'Code:  *[A-Z0-9-]*' "$E/logs/$n-login.log" | awk '{print $2}' || true); [[ -n "$code" ]] && break; sleep 1; done
curl -sf -o /dev/null --data-urlencode "user_code=$code" --data-urlencode "username=relay-lan-$n" http://127.0.0.1:18090/device
for i in $(seq 1 90); do
  "${run[@]}" "$bin" pair -json -state-dir "$H/.mewla" > "$E/$n-pair.json" 2>/dev/null || true
  grep -q '"phone_link": *"mewla://settings?v=3' "$E/$n-pair.json" 2>/dev/null && break; sleep 2
done
grep -q '"phone_link": *"mewla://settings?v=3' "$E/$n-pair.json" || { echo "$n: no Mewla Relay pairing"; tail -20 "$E/logs/$n.log" "$E/logs/$n-login.log"; exit 1; }
hosted=$(grep -o 'https://[a-z0-9-]*\.mewla\.test:19443' "$E/$n-pair.json" | head -1)
for i in $(seq 1 60); do curl -sf -o /dev/null --connect-to ::127.0.0.1: --cacert "$E/certs/pebble-root.pem" "$hosted/health" && break; sleep 2; done
echo "$n: on Mewla Relay at $hosted"
