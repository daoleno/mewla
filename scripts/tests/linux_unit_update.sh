#!/usr/bin/env bash
# `mewla update` and `mewla restart` under a real systemd user unit named
# mewla.service, the one `mewla boot install` writes, on a fresh Linux user.
#
# Runs in a throwaway systemd container (archlinux, memory-capped), so the
# person's own user manager, units and daemon are never involved: the unit
# name is fixed, and `systemctl --user restart mewla.service` on this
# computer's user would restart theirs. Two versions are built from this
# checkout, signed with a throwaway key that only these builds trust, and
# served from a local release server reachable from the container only.
#
#   1. mewla boot install (version A), wait for /health
#   2. spawn a Worker (a stand-in agent that idles) and note its pid and the
#      session host's pid
#   3. mewla update -> B: /health says B, the unit restarted (new MainPID),
#      the session host and the Worker's agent are the same processes
#   4. mewla restart: new MainPID again, the Worker still the same process
#
# Usage: scripts/tests/linux_unit_update.sh [out-dir]
# Needs docker (with archlinux:latest), go, openssl, python3; the
# libghostty-vt cache that scripts/build-libghostty-desktop.sh writes.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${1:-}"
S="$(mktemp -d "${TMPDIR:-/tmp}/mewla-unit.XXXXXX")"
NAME="mewla-sweep-unit-$$"
BRIDGE="$(ip -4 -o addr show docker0 | awk '{print $4}' | cut -d/ -f1)"
RELEASE_PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1])')"
OLD=0.4.90
NEW=0.4.91
SERVER_PID=""
FAILED=0

pass() { printf 'PASS  %s\n' "$*"; [[ -z "$OUT" ]] || printf 'PASS\t%s\n' "$*" >> "$OUT/unit-update.tsv"; }
fail() { printf 'FAIL  %s\n' "$*"; FAILED=1; [[ -z "$OUT" ]] || printf 'FAIL\t%s\n' "$*" >> "$OUT/unit-update.tsv"; }

cleanup() {
  set +e
  docker rm -f "$NAME" >/dev/null 2>&1
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null
  if [[ "${KEEP:-}" != 1 ]]; then rm -rf "$S"; else echo "kept $S"; fi
}
trap cleanup EXIT

# --- two signed releases --------------------------------------------------------
openssl genpkey -algorithm ed25519 -out "$S/key.pem" 2>/dev/null
PUBLIC="$(openssl pkey -in "$S/key.pem" -pubout -outform DER | base64 -w0)"
build() { # build <version> <out>
  (cd "$ROOT/daemon" && CGO_ENABLED=1 \
    CGO_CFLAGS="-I${MEWLA_GHOSTTY_DESKTOP_CACHE:-$HOME/.cache/mewla/ghostty-desktop}/include" \
    CGO_LDFLAGS="-L${MEWLA_GHOSTTY_DESKTOP_CACHE:-$HOME/.cache/mewla/ghostty-desktop}/linux_amd64" \
    go build -tags ghosttyvt -buildvcs=false -ldflags="-X main.Version=$1 -X main.releaseBuild=1 \
-X github.com/daoleno/mewla/daemon/selfupdate.UpdatePublicKeyPKIX=$PUBLIC \
-X github.com/daoleno/mewla/daemon/selfupdate.DefaultReleasesURL=http://$BRIDGE:$RELEASE_PORT/unit/releases" \
    -o "$2" ./cmd/mewla)
}
build "$OLD" "$S/mewla-old"
build "$NEW" "$S/mewla-new"
dir="$S/rel/unit/releases/download/v$NEW"
mkdir -p "$dir" "$S/stage"
cp "$S/mewla-new" "$S/stage/mewla"
tar -C "$S/stage" -czf "$dir/mewla-linux-amd64.tar.gz" mewla
python3 - "$dir" "$NEW" <<'PY'
import hashlib, json, os, sys
d, version = sys.argv[1], sys.argv[2]
data = open(os.path.join(d, "mewla-linux-amd64.tar.gz"), "rb").read()
json.dump({"schema_version": 2, "product": "mewla", "version": version, "artifacts": [{
    "path": "mewla-linux-amd64.tar.gz", "role": "daemon_archive", "sha256": hashlib.sha256(data).hexdigest(),
    "size": len(data), "goos": "linux", "goarch": "amd64"}]}, open(os.path.join(d, "release-manifest.json"), "w"))
PY
openssl pkeyutl -sign -rawin -inkey "$S/key.pem" -in "$dir/release-manifest.json" -out "$dir/release-manifest.json.sig"
echo "$NEW" > "$S/rel/unit/releases/LATEST"
cat > "$S/server.py" <<'PY'
import http.server, os, sys
root, host, port = sys.argv[1], sys.argv[2], int(sys.argv[3])
class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k): super().__init__(*a, directory=root, **k)
    def do_GET(self):
        if self.path.endswith("/releases/latest"):
            name = self.path.split("/")[1]
            tag = open(os.path.join(root, name, "releases", "LATEST")).read().strip()
            self.send_response(302); self.send_header("Location", "/%s/releases/tag/v%s" % (name, tag)); self.end_headers(); return
        super().do_GET()
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer((host, port), H).serve_forever()
PY
python3 -I "$S/server.py" "$S/rel" "$BRIDGE" "$RELEASE_PORT" & SERVER_PID=$!

# --- a fresh machine with systemd ------------------------------------------------
mkdir -p "$S/share"
cp "$S/mewla-old" "$S/share/mewla-old"
cat > "$S/share/agent" <<'EOF'
#!/bin/sh
[ "$1" = --version ] && { echo '2.1.325 (Claude Code)'; exit 0; }
[ "$1" = auth ] && { echo '{"loggedIn":true}'; exit 0; }
echo $$ > "$HOME/agent.pid"
exec sleep 3600
EOF
chmod 755 "$S/share/agent"
# systemd as PID 1 needs its own cgroup namespace, writable (remounted inside
# it, never the host's tree) and a first boot that asks nothing.
docker run -d -t --name "$NAME" --memory 3g --cpus 4 --cgroupns=private --cap-add SYS_ADMIN \
  --security-opt seccomp=unconfined --security-opt apparmor=unconfined --tmpfs /run --tmpfs /run/lock \
  -e container=docker -v "$S/share:/share:ro" archlinux:latest /bin/sh -c \
  'mount -o remount,rw /sys/fs/cgroup && ln -sf /dev/null /etc/systemd/system/systemd-firstboot.service &&
   systemd-machine-id-setup >/dev/null && exec /usr/lib/systemd/systemd' >/dev/null
for _ in $(seq 1 60); do
  state="$(docker exec "$NAME" systemctl is-system-running 2>/dev/null || true)"
  [[ "$state" == running || "$state" == degraded ]] && break
  sleep 1
done
docker exec "$NAME" sh -c 'useradd -m -s /bin/bash sweep && loginctl enable-linger sweep'
for _ in $(seq 1 60); do
  docker exec "$NAME" test -S /run/user/1000/bus && break
  sleep 1
done
as_user() { docker exec -u sweep -w /home/sweep -e HOME=/home/sweep -e XDG_RUNTIME_DIR=/run/user/1000 \
  -e DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus -e PATH=/home/sweep/.local/bin:/usr/bin:/bin "$NAME" sh -c "$1"; }
health() { as_user 'curl -fsS --max-time 2 http://127.0.0.1:9876/health' 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version",""))' 2>/dev/null || true; }
main_pid() { as_user 'systemctl --user show mewla.service -p MainPID --value'; }
wait_version() { for _ in $(seq 1 "$2"); do [[ "$(health)" == "$1" ]] && return 0; sleep 1; done; return 1; }
host_pid() { as_user 'mewla host status --json' | python3 -c 'import json,sys; print(json.load(sys.stdin)["host"]["pid"])'; }
agent_alive() { as_user 'kill -0 "$(cat ~/agent.pid)"' 2>/dev/null; }

as_user 'mkdir -p ~/.local/bin ~/.mewla ~/project && cp /share/mewla-old ~/.local/bin/mewla && cp /share/agent ~/.local/bin/claude &&
  printf "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n" > ~/.mewla/executors.toml'
as_user 'mewla boot install -binary /home/sweep/.local/bin/mewla -addr 127.0.0.1:9876 -work-dir /home/sweep -path-env /home/sweep/.local/bin:/usr/bin:/bin' > "$S/boot-install.txt" 2>&1 || true
if wait_version "$OLD" 90; then pass "boot install: mewla.service runs $OLD"; else fail "boot install: no $OLD on /health"; cat "$S/boot-install.txt"; as_user 'journalctl --user -u mewla.service --no-pager | tail -30'; exit 1; fi
before_main="$(main_pid)"
as_user 'mewla worker spawn -name Keeper -executor claude -cwd /home/sweep/project -prompt "stay" -profile quick' > "$S/spawn.txt" 2>&1 || true
for _ in $(seq 1 30); do agent_alive && break; sleep 1; done
agent_pid="$(as_user 'cat ~/agent.pid' 2>/dev/null || true)"
before_host="$(host_pid)"
if agent_alive; then pass "a Worker runs (agent pid $agent_pid, session host pid $before_host)"; else fail "the Worker never started"; cat "$S/spawn.txt"; fi
as_user 'mewla worker list --json' | grep -q Keeper && pass "the Worker is listed before update" || { fail "the Worker is not listed before update"; as_user 'mewla worker list --json' | head -c 600; }

as_user 'mewla update' > "$S/update.txt" 2>&1 || true
if wait_version "$NEW" 120; then pass "mewla update: /health says $NEW"; else fail "mewla update: still $(health)"; cat "$S/update.txt"; fi
after_main="$(main_pid)"
[[ "$after_main" != "$before_main" && "$after_main" != 0 ]] && pass "the unit restarted onto $NEW (MainPID $before_main -> $after_main)" || fail "MainPID $before_main -> $after_main"
[[ "$(host_pid)" == "$before_host" ]] && pass "update kept the session host ($before_host)" || fail "session host changed: $before_host -> $(host_pid)"
agent_alive && pass "update kept the Worker's agent ($agent_pid)" || fail "the Worker's agent ended on update"
listed() { for _ in $(seq 1 30); do as_user 'mewla worker list --json' 2>/dev/null | grep -q Keeper && return 0; sleep 1; done; return 1; }
listed && pass "the Worker is still listed after update" || { fail "the Worker is not listed after update"; as_user 'mewla worker list --json' | head -c 600; }
printf -- '--- mewla update said:\n%s\n' "$(cat "$S/update.txt")"

as_user 'mewla restart' > "$S/restart.txt" 2>&1 || true
for _ in $(seq 1 60); do [[ "$(main_pid)" != "$after_main" && "$(health)" == "$NEW" ]] && break; sleep 1; done
restart_main="$(main_pid)"
[[ "$restart_main" != "$after_main" && "$(health)" == "$NEW" ]] && pass "mewla restart: new MainPID $restart_main, still $NEW" || fail "mewla restart: MainPID $after_main -> $restart_main, health $(health)"
agent_alive && pass "restart kept the Worker's agent ($agent_pid)" || fail "the Worker's agent ended on restart"
printf -- '--- mewla restart said:\n%s\n' "$(cat "$S/restart.txt")"
as_user 'journalctl --user -u mewla.service --no-pager -o cat' > "$S/journal.txt" 2>&1 || true
[[ -z "$OUT" ]] || cp "$S/update.txt" "$S/restart.txt" "$S/journal.txt" "$OUT/" 2>/dev/null || true
exit "$FAILED"
