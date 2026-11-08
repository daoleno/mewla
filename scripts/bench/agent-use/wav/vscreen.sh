#!/usr/bin/env bash
# The agent browser on the virtual screen (Settings -> Labs -> Virtual screen), for the agent-use benchmark and the live
# proofs, behind an ISOLATED daemon: its own state dir, port and tmux server; the live daemon, its tmux and its Brain are
# never touched. The Workers get the one browser tool every Worker gets, mewla_chrome (the Mewla extension, here in the
# agent browser's own Chrome on the agent screen, an Xvnc with a monitor per agent).
#
#   vscreen.sh start DIR [N]  build mewla, start the daemon with the switch on, spawn N Workers (default 1; stand-in
#                             `claude` programs that only idle, so the daemon builds the real tool attachment for them)
#                             and write DIR/env.sh and DIR/env.json with VS_BIN, VS_STATE, VS_HOME, VS_WORKER (the
#                             first) and VS_WORKERS, and DIR/mcp-<worker>.json with each Worker's own --mcp-config
#   vscreen.sh stop DIR       stop that daemon and everything it started, remove DIR
#   vscreen.sh restart DIR    stop, then start again in DIR (new Worker ids in DIR/env.json)
#
# The agent then runs the entry in DIR/mcp-<worker>.json (`mewla agent-use chrome-mcp --state-dir VS_STATE`) with
# MEWLA_WORKER_ID set to that Worker, which is exactly the entry the daemon puts in a Worker's --mcp-config. Needs Chrome,
# Xvnc, xauth and Node (the tool's own prerequisites, `Capability` in daemon/browser/chrome_linux.go).
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/../../../.." && pwd)
cmd=${1:?start|stop}; V=${2:?DIR}
PORT=${VS_PORT:-9971}
TMUXBIN=$(command -v tmux)
NODEBIN=$(dirname "$(command -v node)")
case $cmd in
start)
  N=${3:-1}
  [ ! -e "$V" ] || { echo "$V exists" >&2; exit 1; }
  mkdir -p "$V/bin" "$V/home/.mewla" "$V/state/browsers" "$V/cwd"
  (cd "$REPO/daemon" && go build -o "$V/mewla" ./cmd/mewla)
  # The pinned Playwright MCP server, from the live install when there is one (the daemon would fetch the same files).
  if [ -f "$HOME/.mewla/agent-use/playwright-mcp/.mewla-installed" ]; then
    mkdir -p "$V/state/agent-use" && cp -a "$HOME/.mewla/agent-use/playwright-mcp" "$V/state/agent-use/playwright-mcp"
  fi
  cat >"$V/bin/claude" <<STUB
#!/bin/bash
# Stand-in for the provider CLI: records how the daemon launched it, then idles like a Worker waiting for work.
case "\$1" in --version) echo "2.1.300 (Claude Code)"; exit 0;; auth) echo '{"loggedIn":true}'; exit 0;; esac
printf '%s\0' "\$@" > "$V/launch-\${MEWLA_WORKER_ID:-unknown}.argv"
exec sleep 100000
STUB
  chmod +x "$V/bin/claude"
  printf 'delegated_executor = "claude"\n\n[[executors]]\nname = "claude"\ncommand = "claude --model claude-sonnet-5-5"\n' >"$V/home/.mewla/executors.toml"
  echo '{"virtual_screen":true}' >"$V/state/browsers/settings.json"
  ENVI="env -i HOME=$V/home PATH=$V/bin:$NODEBIN:/usr/local/bin:/usr/bin:/bin"
  "$TMUXBIN" -S "$V/tmux.sock" new-session -d -s vs -x 200 -y 50 "$ENVI TERM=xterm-256color MEWLA_BRAIN_HOST_EXECUTOR=agent TMUX=\"\$TMUX\" $V/mewla serve -addr 127.0.0.1:$PORT -state-dir $V/state > $V/daemon.log 2>&1"
  for _ in $(seq 30); do grep -q "listening on" "$V/daemon.log" 2>/dev/null && break; sleep 1; done
  grep -q "listening on" "$V/daemon.log" || { echo "isolated daemon did not start; see $V/daemon.log" >&2; exit 1; }
  # The tool is attached once the daemon says the browser is ready (the browser tools are fetched first when missing).
  for _ in $(seq 120); do
    $ENVI "$V/mewla" agent-use status -json -state-dir "$V/state" 2>/dev/null | grep -q '"state":"ready"' && break; sleep 1
  done
  WIDS=()
  for i in $(seq "$N"); do
    $ENVI "$V/mewla" worker spawn -executor claude -name "aub-vscreen-$i" -cwd "$V/cwd" -prompt "idle" -state-dir "$V/state" >"$V/spawn-$i.json"
    WIDS+=("$(python3 -I -c "import json;print(json.load(open('$V/spawn-$i.json'))['worker']['id'])")")
  done
  for w in "${WIDS[@]}"; do
    for _ in $(seq 30); do [ -s "$V/launch-$w.argv" ] && break; sleep 1; done
    [ -s "$V/launch-$w.argv" ] || { echo "Worker $w was not launched; see $V/spawn-*.json, $V/daemon.log" >&2; exit 1; }
    python3 -I - "$V" "$w" <<'PY'
import json, sys
v, w = sys.argv[1], sys.argv[2]
a = open(f'{v}/launch-{w}.argv', 'rb').read().split(b'\0')
cfg = json.loads(a[a.index(b'--mcp-config') + 1])
json.dump(cfg, open(f'{v}/mcp-{w}.json', 'w'))
print(w, 'tools:', ', '.join(sorted(cfg['mcpServers'])))
PY
  done
  printf 'export VS_BIN=%s VS_STATE=%s VS_HOME=%s VS_WORKER=%q VS_WORKERS=%q\n' "$V/mewla" "$V/state" "$V/home" "${WIDS[0]}" "${WIDS[*]}" >"$V/env.sh"
  python3 -I -c "import json;print(json.dumps({'VS_BIN':'$V/mewla','VS_STATE':'$V/state','VS_HOME':'$V/home','VS_WORKER':'${WIDS[0]}','VS_WORKERS':'${WIDS[*]}'.split()}))" >"$V/env.json"
  echo "isolated daemon on 127.0.0.1:$PORT, Workers ${WIDS[*]}"
  ;;
stop)
  [ -d "$V" ] || exit 0
  # Everything below the daemon's pid, collected before it goes (a browser's display and helpers are reparented once it
  # does): only this daemon's own tree, never a browser another daemon runs.
  tree() { local p; for p in $(pgrep -P "$1" 2>/dev/null || true); do echo "$p"; tree "$p"; done; }
  DAEMONS=$(pgrep -f -- "$V/mewla serve" || true)
  KIDS=""; for d in $DAEMONS; do KIDS="$KIDS $(tree "$d")"; done
  # The Chrome of this daemon's agent profile, and the programs the Workers' tools started.
  STRAYS=$(pgrep -f -- "$V/state" || true)
  "$TMUXBIN" -S "$V/tmux.sock" kill-server 2>/dev/null || true
  for p in $DAEMONS; do kill "$p" 2>/dev/null || true; done
  sleep 3
  for p in $KIDS $STRAYS; do kill "$p" 2>/dev/null || true; done
  sleep 1
  rm -rf "$V"
  ;;
restart)
  # A fresh daemon and Workers in the same directory (the benchmark does this when the browser has died).
  "$0" stop "$V"; "$0" start "$V" "${3:-1}";;
*) echo "usage: vscreen.sh start|stop|restart DIR [N]" >&2; exit 2;;
esac
