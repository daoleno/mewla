#!/usr/bin/env bash
# The virtual-screen browser of daemon/browser (Settings -> Labs -> Virtual screen), for the agent-use benchmark, behind an
# ISOLATED daemon: its own state dir, port and tmux server; the live daemon, its tmux and its Brain are never touched.
#
#   vscreen.sh start DIR   build mewla, start the daemon with the switch on, spawn one Worker (a stand-in `claude` that
#                          only idles, so the daemon builds the real browser attachment for it) and write DIR/env.sh
#                          with VS_BIN, VS_STATE, VS_HOME, VS_WORKER, VS_BROWSER for suite.mjs --side vscreen
#   vscreen.sh stop DIR    stop that daemon and everything it started, remove DIR
#   vscreen.sh restart DIR stop, then start again in DIR (new Worker and browser ids in DIR/env.json)
#
# The agent then runs `mewla browser mcp --id VS_BROWSER --state-dir VS_STATE` with MEWLA_WORKER_ID=VS_WORKER, which is
# exactly the entry the daemon puts in a Worker's --mcp-config. Needs Chrome, kwin_wayland, dbus-run-session and
# agent-browser (the browser's own prerequisites, `Capability` in daemon/browser/runtime_linux.go).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../../../.." && pwd)
cmd=${1:?start|stop}; V=${2:?DIR}
PORT=${VS_PORT:-9971}
TMUXBIN=$(command -v tmux)
NODEBIN=$(dirname "$(command -v agent-browser)")
case $cmd in
start)
  [ ! -e "$V" ] || { echo "$V exists" >&2; exit 1; }
  mkdir -p "$V/bin" "$V/home/.mewla" "$V/state/browsers" "$V/cwd"
  (cd "$REPO/daemon" && go build -o "$V/mewla" ./cmd/mewla)
  cat >"$V/bin/claude" <<STUB
#!/bin/bash
# Stand-in for the provider CLI: records how the daemon launched it, then idles like a Worker waiting for work.
case "\$1" in --version) echo "2.1.300 (Claude Code)"; exit 0;; auth) echo '{"loggedIn":true}'; exit 0;; esac
printf '%s\0' "\$@" > "$V/launch-argv"
exec sleep 100000
STUB
  chmod +x "$V/bin/claude"
  printf 'delegated_executor = "claude"\n\n[[executors]]\nname = "claude"\ncommand = "claude --model claude-sonnet-5-5"\n' >"$V/home/.mewla/executors.toml"
  echo '{"virtual_screen":true}' >"$V/state/browsers/settings.json"
  ENVI="env -i HOME=$V/home PATH=$V/bin:$NODEBIN:/usr/local/bin:/usr/bin:/bin"
  "$TMUXBIN" -S "$V/tmux.sock" new-session -d -s vs -x 200 -y 50 "$ENVI TERM=xterm-256color MEWLA_BRAIN_HOST_EXECUTOR=agent TMUX=\"\$TMUX\" $V/mewla serve -addr 127.0.0.1:$PORT -state-dir $V/state > $V/daemon.log 2>&1"
  for _ in $(seq 30); do grep -q "listening on" "$V/daemon.log" 2>/dev/null && break; sleep 1; done
  grep -q "listening on" "$V/daemon.log" || { echo "isolated daemon did not start; see $V/daemon.log" >&2; exit 1; }
  $ENVI "$V/mewla" worker spawn -executor claude -name aub-vscreen -cwd "$V/cwd" -prompt "idle" -state-dir "$V/state" >"$V/spawn.json"
  for _ in $(seq 30); do [ -s "$V/launch-argv" ] && break; sleep 1; done
  [ -s "$V/launch-argv" ] || { echo "the Worker was not launched; see $V/spawn.json, $V/daemon.log" >&2; exit 1; }
  WID=$(python3 -I -c "import json;print(json.load(open('$V/spawn.json'))['worker']['id'])")
  BID=$(python3 -I - <<PY
import json
a=open('$V/launch-argv','rb').read().split(b'\0')
cfg=json.loads(a[a.index(b'--mcp-config')+1])
args=cfg['mcpServers']['mewla_browser']['args']
print(args[args.index('--id')+1])
PY
)
  printf 'export VS_BIN=%s VS_STATE=%s VS_HOME=%s VS_WORKER=%q VS_BROWSER=%s\n' "$V/mewla" "$V/state" "$V/home" "$WID" "$BID" >"$V/env.sh"
  python3 -I -c "import json;print(json.dumps({'VS_BIN':'$V/mewla','VS_STATE':'$V/state','VS_HOME':'$V/home','VS_WORKER':'$WID','VS_BROWSER':'$BID'}))" >"$V/env.json"
  echo "isolated daemon on 127.0.0.1:$PORT, Worker $WID, browser $BID"
  ;;
stop)
  [ -d "$V" ] || exit 0
  # The daemon's own children go with it; the tmux server of this directory only.
  # Everything below the daemon's pid, collected before it goes (a browser's display and helpers are reparented once it
  # does): only this daemon's own tree, never a browser another daemon runs.
  tree() { local p; for p in $(pgrep -P "$1" 2>/dev/null || true); do echo "$p"; tree "$p"; done; }
  DAEMONS=$(pgrep -f -- "$V/mewla serve" || true)
  KIDS=""; for d in $DAEMONS; do KIDS="$KIDS $(tree "$d")"; done
  "$TMUXBIN" -S "$V/tmux.sock" kill-server 2>/dev/null || true
  for p in $DAEMONS; do kill "$p" 2>/dev/null || true; done
  sleep 3
  for p in $KIDS; do kill "$p" 2>/dev/null || true; done
  sleep 1
  rm -rf "$V"
  ;;
restart)
  # A fresh daemon, Worker and browser in the same directory (the benchmark does this when the browser has died).
  "$0" stop "$V"; "$0" start "$V";;
*) echo "usage: vscreen.sh start|stop|restart DIR" >&2; exit 2;;
esac
