#!/usr/bin/env bash
# The release gate for "pick each agent's model": walks the model picker
# matrix (agent x moment) against the REAL agents installed on this computer,
# signed in as they are, on an isolated daemon, and prints one row per cell
# with the agent's own record of the turn that followed the pick.
#
#   scripts/model-picker-gate.sh [--agents claude,codex,...] [--routes] [--ask] [--keep]
#
# --routes copies this computer's default Claude and Codex Provider
#   connections (~/.mewla/model-profiles.toml defaults) into the scratch state
#   (0600, never printed), so routed Sessions are walked too: the owner's
#   default. Without it the agents run on their own sign-ins.
# --ask starts Claude asking before it runs a command: its busy moment is
#   then a wait for an answer, which the gate gives after the pick.
# --keep leaves the scratch daemon running and its folder in place.
#
# It never touches the live daemon: own state root, own port, env -i, and
# every process it starts is stopped by pid. It leaves this computer's Codex
# config and Claude Code settings alone (MEWLA_NO_CONFIG_TAKEOVER) and fails
# if either changed during the run. Spend: a few one-line turns per
# agent on cheap models (cmd/mewla-model-gate defaultTargets), well under
# 5 USD for the whole matrix. Not for hosted CI: it needs the agents'
# sign-ins. Run it before a release that touches model switching
# (docs/ci-release.md).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
AGENTS="claude,codex,opencode,pi,cursor,grok,antigravity,dsh"
ROUTES=0
KEEP=0
GATE_ARGS=()
PORT=${MEWLA_GATE_PORT:-9979}
while [ $# -gt 0 ]; do
  case "$1" in
    --agents) AGENTS=$2; shift 2 ;;
    --routes) ROUTES=1; shift ;;
    --keep) KEEP=1; shift ;;
    --ask) GATE_ARGS+=(-ask); shift ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
if ss -ltn 2>/dev/null | grep -q "127.0.0.1:$PORT "; then
  echo "port $PORT is taken; set MEWLA_GATE_PORT" >&2
  exit 1
fi

# A short real path: Codex's control socket must stay under 108 bytes.
S=$(mktemp -d /tmp/mpg.XXXXXX)
case "$S" in /tmp/mpg.??????) ;; *) echo "unexpected scratch path $S" >&2; exit 1 ;; esac
mkdir -p "$S/bin" "$S/state" "$S/work" "$S/tmux"
printf '#!/bin/sh\nexit 1\n' > "$S/bin/systemd-run"   # keep the session host out of user units
chmod +x "$S/bin/systemd-run"

echo "building the daemon and the gate into $S/bin" >&2
GHOSTTY=${MEWLA_GHOSTTY_DESKTOP:-$HOME/.cache/mewla/ghostty-desktop}
(cd "$ROOT/daemon" &&
  CGO_ENABLED=1 CGO_CFLAGS="-I$GHOSTTY/include" CGO_LDFLAGS="-L$GHOSTTY/linux_amd64" \
    go build -tags ghosttyvt -o "$S/bin/mewla" ./cmd/mewla &&
  go build -o "$S/bin/mewla-model-gate" ./cmd/mewla-model-gate)

cat > "$S/state/executors.toml" <<'EOF'
[[executors]]
name = "claude"
command = "claude --model haiku"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "opencode"
command = "opencode"

[[executors]]
name = "pi"
command = "pi"
kind = "pi"

[[executors]]
name = "agent"
command = "cursor-agent"
kind = "cursor"

[[executors]]
name = "grok"
command = "grok"
kind = "grok"

[[executors]]
name = "brainstub"
command = "cat"
EOF

if [ "$ROUTES" = 1 ]; then
  python3 -I - "$HOME/.mewla" "$S/state" <<'PY'
import json, os, sys, tomllib
src, dst = sys.argv[1], sys.argv[2]
text = open(os.path.join(src, "model-profiles.toml")).read()
profiles = tomllib.loads(text)
want = [v for k, v in profiles.get("defaults", {}).items() if k in ("claude", "codex")]
blocks = [b.split("[defaults]")[0] for b in text.split("[[profiles]]")[1:] if any('id = "%s"' % w in b for w in want)]
out = "revision = %d\n\n" % profiles["revision"] + "".join("[[profiles]]" + b.rstrip() + "\n\n" for b in blocks)
out += "[defaults]\n" + "".join('  %s = "%s"\n' % (k, v) for k, v in profiles["defaults"].items() if k in ("claude", "codex"))
creds = json.load(open(os.path.join(src, "provider-credentials.json")))
creds["secrets"] = {k: v for k, v in creds["secrets"].items() if any(w in k for w in want)}
for name, body in (("model-profiles.toml", out), ("provider-credentials.json", json.dumps(creds))):
    fd = os.open(os.path.join(dst, name), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.write(fd, body.encode())
    os.close(fd)
print("routes: copied %d connection(s)" % len(blocks), file=sys.stderr)
PY
  # Codex gets a home of its own, so the daemon's gateway is projected into a
  # copy of this computer's Codex config (as the person's daemon does into
  # theirs) and its Sessions start the same way. The gateway's provider needs
  # no sign-in file.
  mkdir -p "$S/codex-home"
  install -m 600 "$HOME/.codex/config.toml" "$S/codex-home/config.toml"
  export CODEX_HOME="$S/codex-home"
fi

AGENT_PATH="$S/bin:$HOME/.opencode/bin:$HOME/.grok/bin:$HOME/.local/bin"
for tool in codex pi dsh node; do
  if found=$(command -v "$tool" 2>/dev/null) && [ -x "$found" ]; then AGENT_PATH="$AGENT_PATH:$(dirname "$found")"; fi
done
AGENT_PATH="$AGENT_PATH:/usr/local/bin:/usr/bin:/bin"
up() {
  (cd "$S/work" || exit 1
  env -i HOME="$HOME" USER="$USER" LANG=en_US.UTF-8 TERM=xterm-256color SHELL=/bin/zsh \
    PATH="$AGENT_PATH" XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-}" DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-}" \
    TMUX_TMPDIR="$S/tmux" MEWLA_STATE_ROOT="$S/state" MEWLA_NO_AUTOUPDATE=1 MEWLA_BRAIN_HOST_EXECUTOR=brainstub \
    MEWLA_NO_CONFIG_TAKEOVER=1 ${CODEX_HOME:+CODEX_HOME="$CODEX_HOME"} \
    "$S/bin/mewla" serve -addr "127.0.0.1:$PORT" -loopback-only -state-dir "$S/state" >> "$S/daemon.log" 2>&1 < /dev/null &
    echo $! > "$S/daemon.pid")
}
down() {
  local pid
  pid=$(cat "$S/daemon.pid" 2>/dev/null || true)
  [ -n "$pid" ] || return 0
  kill "$pid" 2>/dev/null || true
  # serve can take minutes to stop on SIGTERM; this one is ours, so it is ended.
  for _ in $(seq 1 120); do kill -0 "$pid" 2>/dev/null || return 0; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  sleep 1
}
cleanup() {
  [ "$KEEP" = 1 ] && { echo "kept: $S (daemon pid $(cat "$S/daemon.pid"))" >&2; return; }
  local host
  host=$(env -i HOME="$HOME" PATH=/usr/bin:/bin XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-}" MEWLA_STATE_ROOT="$S/state" \
    "$S/bin/mewla" host status --state-dir "$S/state" --json 2>/dev/null || true)
  down
  if [ -n "$host" ]; then
    # Every Session's program, then the host itself, by the pids it reports.
    python3 -I -c 'import json,sys; d=json.loads(sys.argv[1]); print(d["host"]["pid"], *[s["pid"] for s in d.get("sessions",[]) if s.get("pid")])' "$host" |
      while read -r host_pid pids; do
        for pid in $pids; do kill "$pid" 2>/dev/null || true; done
        sleep 2
        kill "$host_pid" 2>/dev/null || true
      done
  fi
  # An agent's helper the host does not list (Codex's app-server) still runs
  # from the scratch folder: end it by pid too.
  for pid in $(pgrep -f -- "$S/" || true); do
    [ "$pid" != "$$" ] && kill "$pid" 2>/dev/null || true
  done
  [ -f "$S/pair.log" ] && sed -E "s/(p%3D|p=)[A-Za-z0-9_%-]+/\1<redacted>/g" "$S/pair.log" | tail -5 >&2
  case "$S" in /tmp/mpg.??????) rm -rf -- "$S" ;; esac
}
# The person's own agent configs: the run must leave them byte for byte.
OWN_CONFIGS="$HOME/.codex/config.toml $HOME/.claude/settings.json"
configs_sum() { for f in $OWN_CONFIGS; do [ -f "$f" ] && sha256sum "$f"; done; }
CONFIGS_BEFORE=$(configs_sum)
finish() {
  local rc=$?
  cleanup
  if [ "$(configs_sum)" != "$CONFIGS_BEFORE" ]; then
    echo "FAIL: the run changed $OWN_CONFIGS" >&2
    exit 1
  fi
  exit $rc
}
trap finish EXIT

up
for _ in $(seq 1 60); do curl -fsS "http://127.0.0.1:$PORT/health" >/dev/null 2>&1 && break; sleep 1; done
LINK=""
for _ in $(seq 1 30); do   # the control socket comes up a moment after /health
  LINK=$(env -i HOME="$HOME" PATH=/usr/bin:/bin MEWLA_STATE_ROOT="$S/state" "$S/bin/mewla" pair -details -state-dir "$S/state" 2>&1 | tee -a "$S/pair.log" |
    grep -o "http://127.0.0.1:$PORT/#pair=[^ ]*" | head -1 || true)
  [ -n "$LINK" ] && break
  sleep 2
done
[ -n "$LINK" ] || { echo "the isolated daemon gave no pairing link" >&2; exit 1; }
"$S/bin/mewla-model-gate" pair -link "$LINK" -server "http://127.0.0.1:$PORT" -key "$S/key.json"
cat > "$S/restart.sh" <<EOF
#!/usr/bin/env bash
$(declare -f up down)
S=$S PORT=$PORT AGENT_PATH="$AGENT_PATH" ${CODEX_HOME:+CODEX_HOME="$CODEX_HOME"}
down
up
EOF
chmod +x "$S/restart.sh"
"$S/bin/mewla-model-gate" run -server "http://127.0.0.1:$PORT" -key "$S/key.json" -cwd "$S/work" \
  -agents "$AGENTS" -restart "$S/restart.sh" -out "${MEWLA_GATE_OUT:-/dev/null}" "${GATE_ARGS[@]}"
