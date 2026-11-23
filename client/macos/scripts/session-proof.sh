#!/usr/bin/env bash
# A Session with a live process kept across a daemon change (macos-permissions.yml: the old job moved onto the app's
# executable; the daemon restarting itself in place after a grant). A Worker runs a counter in its terminal, one line
# a second, also appended to a file; `check` proves the same Session id, the same process, and output still coming
# through the daemon (the Worker's capture) and from the process itself (the file).
#   session-proof.sh start <mewla cli> <dir>   (dir on durable storage: the daemon refuses a Worker in a temp dir)
#   session-proof.sh check <mewla cli> <dir> <label>
set -euo pipefail
GITHUB_ACTIONS="${GITHUB_ACTIONS:-}"
[[ "$GITHUB_ACTIONS" == true ]] || { echo "session-proof.sh runs on a CI Mac only" >&2; exit 2; }
mode="${1:?start|check}"
cli="${2:?mewla cli}"
dir="${3:?dir}"
mkdir -p "$dir"

sessions() { "$cli" host status --json | python3 -I -c 'import json,sys; print(" ".join(sorted("%s:%s" % (s["id"], s["pid"]) for s in json.load(sys.stdin).get("sessions", []))))'; }
last_tick() { grep -o 'tick [0-9]*' | tail -1 | grep -o '[0-9]*' || echo 0; }
captured() { "$cli" worker capture -id "$(cat "$dir/worker-id")" --json 2>/dev/null | last_tick; }
written() { last_tick < "$dir/ticks.txt"; }

case "$mode" in
  start)
    # Quiet at first: the spawn hands the Worker its brief once the terminal is still, as at an agent's prompt.
    printf '#!/bin/sh\nsleep 20\ni=0\nwhile :; do i=$((i+1)); echo "tick $i"; echo "tick $i" >> "%s"; sleep 1; done\n' "$dir/ticks.txt" > "$dir/tick.sh"
    chmod 755 "$dir/tick.sh"
    : > "$dir/ticks.txt"
    before=" $(sessions) "
    "$cli" worker spawn -name "Session proof" -executor claude -command "$dir/tick.sh" -cwd "$dir" > "$dir/spawn.json"
    mine=""
    for _ in $(seq 1 90); do
      # The Worker named so (its id is its Session's), among the Sessions that were not there before.
      wid=$("$cli" worker list --json 2>/dev/null | python3 -I -c '
import json, sys
def walk(v):
    if isinstance(v, dict):
        if str(v.get("name", "")).startswith("Session proof") and str(v.get("id", "")).startswith("mh-"):
            print(v["id"]); sys.exit(0)
        for x in v.values(): walk(x)
    elif isinstance(v, list):
        for x in v: walk(x)
walk(json.load(sys.stdin))' || true)
      for one in $(sessions); do [[ -n "$wid" && "$one" == "$wid":* && "$before" != *" $one "* ]] && mine="$one"; done
      [[ -n "$mine" ]] && [[ $(written) -ge 2 ]] && break
      sleep 1
    done
    cat "$dir/spawn.json"
    echo "${mine%%:*}" > "$dir/worker-id"
    test -n "$mine"
    echo "$mine" > "$dir/session"
    pgrep -f "$dir/tick.sh" | sort | tr '\n' ' ' > "$dir/tick-pid"
    echo "session $mine, counter pid $(cat "$dir/tick-pid"), tick $(written), captured tick $(captured)"
    ;;
  check)
    label="${4:?label}"
    mine=$(cat "$dir/session")
    for _ in $(seq 1 90); do [[ " $(sessions 2>/dev/null || true) " == *" $mine "* ]] && break; sleep 1; done
    now=$(sessions)
    [[ " $now " == *" $mine "* ]] || { echo "$label: Session $mine is gone (now: $now)"; exit 1; }
    pid_now=$(pgrep -f "$dir/tick.sh" | sort | tr '\n' ' ')
    [[ "$pid_now" == "$(cat "$dir/tick-pid")" ]] || { echo "$label: the counter's process changed: $(cat "$dir/tick-pid")-> $pid_now"; exit 1; }
    a=$(written); c1=$(captured)
    sleep 4
    b=$(written); c2=$(captured)
    [[ $b -gt $a ]] || { echo "$label: the counter stopped at $a"; exit 1; }
    [[ $c2 -gt $c1 && $c2 -ge $a ]] || { echo "$label: the daemon's capture of the Session stopped at $c1 -> $c2 (process at $b)"; exit 1; }
    echo "$label: the same Session $mine, the same process $pid_now, output continues (file $a -> $b, through the daemon $c1 -> $c2)"
    "$cli" worker list --json | grep -q "Session proof" || { echo "$label: the daemon does not list the Worker"; exit 1; }
    ;;
  *) echo "start|check" >&2; exit 2 ;;
esac
