#!/usr/bin/env bash
# End-to-end proof of the daemon's self-update under a real systemd user unit.
#
# Every scenario is an isolated daemon: its own HOME (so its own Brain root),
# state directory, port, tmux server and transient unit named mewla-e2e-*. The
# releases come from a local server and are signed with a throwaway test key
# that the test binaries trust (-X on selfupdate.UpdatePublicKeyPKIX and
# DefaultReleasesURL); production builds never carry either.
#
#   good      0.2.90 -> 0.2.91 (a real build): installs, same PID, .prev kept
#   bad       0.2.92 answers `version` but exits 1 when started: rolled back
#   hang      0.2.93 starts and never listens: rolled back after the gate
#   powercut  the whole unit is SIGKILLed mid-handover: the next start is the
#             old version again, with the databases put back
#
# Usage: scripts/tests/autoupdate-e2e.sh [good bad hang powercut]
# Takes about four minutes (the daemon's first update check is 2 minutes after
# it starts). Needs systemd --user, tmux, openssl, python3, curl.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCENARIOS=("$@")
[[ ${#SCENARIOS[@]} -gt 0 ]] || SCENARIOS=(good bad hang powercut)

S="$(mktemp -d "${TMPDIR:-/tmp}/mewla-e2e.XXXXXX")"
RELEASE_PORT=9970
declare -A PORT=([good]=9981 [bad]=9982 [hang]=9983 [powercut]=9984)
declare -A FAKE_VERSION=([bad]=0.2.92 [hang]=0.2.93 [powercut]=0.2.94)
OLD_VERSION=0.2.90
GOOD_VERSION=0.2.91
UNITS=()
SERVER_PID=""
FAILED=0

log() { printf '\n== %s\n' "$*"; }
pass() { printf 'PASS  %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*"; FAILED=1; }
check() { # check "description" command...
  local what="$1"; shift
  if "$@"; then pass "$what"; else fail "$what"; fi
}

cleanup() {
  set +e
  for unit in "${UNITS[@]}"; do
    # KillMode=process leaves anything but the main process behind on a plain
    # stop; the test owns the whole unit, so take it all down.
    systemctl --user kill -s KILL --kill-whom=all "$unit" 2>/dev/null
    systemctl --user stop "$unit" 2>/dev/null
    rm -f "$HOME/.config/systemd/user/$unit"
  done
  if [[ ${#UNITS[@]} -gt 0 ]]; then
    systemctl --user daemon-reload
    # The SIGKILL above leaves the units in a failed state that would linger.
    systemctl --user reset-failed "${UNITS[@]}" 2>/dev/null
  fi
  for scenario in "${SCENARIOS[@]}"; do
    /usr/bin/tmux -S "$S/$scenario/tmux.sock" kill-server 2>/dev/null
  done
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null
  if [[ "${KEEP:-}" != 1 ]]; then rm -rf "$S"; else echo "kept $S"; fi
}
trap cleanup EXIT

unit_name() { echo "mewla-e2e-$1-$$.service"; }
for scenario in "${SCENARIOS[@]}"; do UNITS+=("$(unit_name "$scenario")"); done
absent() { for path in "$@"; do [ ! -e "$path" ] || return 1; done; }

# --- test key, release server, release files ---------------------------------
openssl genpkey -algorithm ed25519 -out "$S/key.pem" 2>/dev/null
PUBLIC="$(openssl pkey -in "$S/key.pem" -pubout -outform DER | base64 -w0)"

ldflags() { # ldflags <version> <scenario>
  echo "-X main.Version=$1 -X main.releaseBuild=1 \
-X github.com/daoleno/mewla/daemon/selfupdate.UpdatePublicKeyPKIX=$PUBLIC \
-X github.com/daoleno/mewla/daemon/selfupdate.DefaultReleasesURL=http://127.0.0.1:$RELEASE_PORT/$2/releases"
}
build() { # build <version> <scenario> <out>
  (cd "$ROOT/daemon" && CGO_ENABLED=0 go build -buildvcs=false -ldflags="$(ldflags "$1" "$2")" -o "$3" ./cmd/mewla)
}

make_release() { # make_release <scenario> <version> <binary>
  local dir="$S/rel/$1/releases/download/v$2"
  mkdir -p "$dir"
  local stage; stage="$(mktemp -d "$S/stage.XXXX")"
  cp "$3" "$stage/mewla"
  tar -C "$stage" -czf "$dir/mewla-linux-amd64.tar.gz" mewla
  python3 - "$dir" "$2" <<'PY'
import hashlib, json, os, sys
d, version = sys.argv[1], sys.argv[2]
a = os.path.join(d, "mewla-linux-amd64.tar.gz")
data = open(a, "rb").read()
manifest = {"schema_version": 2, "product": "mewla", "version": version, "artifacts": [{
    "path": "mewla-linux-amd64.tar.gz", "role": "daemon_archive",
    "sha256": hashlib.sha256(data).hexdigest(), "size": len(data), "goos": "linux", "goarch": "amd64"}]}
open(os.path.join(d, "release-manifest.json"), "w").write(json.dumps(manifest))
PY
  openssl pkeyutl -sign -rawin -inkey "$S/key.pem" -in "$dir/release-manifest.json" -out "$dir/release-manifest.json.sig"
  echo "$2" > "$S/rel/$1/releases/LATEST"
}

fake_binary() { # fake_binary <path> <version> <what a daemon start does>
  cat > "$1" <<EOF
#!/bin/sh
if [ "\$1" = version ]; then echo '{"version":"$2"}'; exit 0; fi
$3
EOF
  chmod +x "$1"
}

cat > "$S/server.py" <<'PY'
import http.server, os, sys
root, port = sys.argv[1], int(sys.argv[2])
class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k): super().__init__(*a, directory=root, **k)
    def do_GET(self):
        with open(os.path.join(root, "access.log"), "a") as f: f.write(self.path + "\n")
        parts = self.path.split("/")
        if self.path.endswith("/releases/latest"):
            tag = open(os.path.join(root, parts[1], "releases", "LATEST")).read().strip()
            self.send_response(302)
            self.send_header("Location", "/%s/releases/tag/v%s" % (parts[1], tag)); self.end_headers(); return
        super().do_GET()
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
PY
mkdir -p "$S/rel"
python3 "$S/server.py" "$S/rel" "$RELEASE_PORT" & SERVER_PID=$!

# --- one isolated daemon under a real systemd user unit ----------------------
start_daemon() { # start_daemon <scenario> <old binary>
  local name="$1" dir="$S/$1"
  mkdir -p "$dir/home" "$dir/bin"
  cp "$2" "$dir/bin/mewla"
  # The Brain root, the state directory and the tmux server are all private;
  # TMUX must name the scratch server or the daemon would use the live one.
  env -i /usr/bin/tmux -S "$dir/tmux.sock" new-session -d -s e2e
  HOME="$dir/home" "$dir/bin/mewla" boot install -dry-run -binary "$dir/bin/mewla" \
    -state-dir "$dir/home/.mewla" -addr "127.0.0.1:${PORT[$name]}" -work-dir "$dir" \
    -path-env /usr/local/bin:/usr/bin:/bin > "$dir/unit.rendered"
  local unit; unit="$(unit_name "$name")"
  {
    sed '/^\[Install\]/,$d' "$dir/unit.rendered"
    echo "Environment=\"TMUX=$dir/tmux.sock\""
    echo "Environment=\"MEWLA_BRAIN_HOST_EXECUTOR=agent\""
    echo "RestartSec=2"
  } | grep -v '^RestartSec=5' > "$dir/$unit"
  mkdir -p "$HOME/.config/systemd/user"
  ln -sf "$dir/$unit" "$HOME/.config/systemd/user/$unit"
  systemctl --user daemon-reload
  systemctl --user start "$unit"
}

unit_of() { unit_name "$1"; }
main_pid() { systemctl --user show "$(unit_of "$1")" -p MainPID --value; }
restarts() { systemctl --user show "$(unit_of "$1")" -p NRestarts --value; }
health_version() { curl -fsS --max-time 2 "http://127.0.0.1:${PORT[$1]}/health" 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version",""))' 2>/dev/null || true; }
wait_for() { # wait_for <seconds> <description> command...
  local limit="$1" what="$2"; shift 2
  local end=$((SECONDS + limit))
  until "$@"; do
    if ((SECONDS > end)); then echo "timed out waiting for: $what"; return 1; fi
    sleep 1
  done
}
healthy_as() { [[ "$(health_version "$1")" == "$2" ]]; }
binary_version() { "$S/$1/bin/mewla" version --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])'; }
journal() { journalctl --user -u "$(unit_of "$1")" --no-pager -o cat 2>/dev/null | tail -n "${2:-40}"; }
hit_local_server() { grep -q "^/$1/releases/" "$S/rel/access.log" 2>/dev/null; }

old_build() { build "$OLD_VERSION" "$1" "$S/old-$1"; }

# --- scenarios ----------------------------------------------------------------
scenario_good() {
  log "good: $OLD_VERSION -> $GOOD_VERSION, a real build"
  old_build good
  build "$GOOD_VERSION" good "$S/new-good"
  make_release good "$GOOD_VERSION" "$S/new-good"
  start_daemon good "$S/old-good"
  wait_for 90 "old daemon up" healthy_as good "$OLD_VERSION" || { fail "old daemon never came up"; journal good; return; }
  local pid; pid="$(main_pid good)"
  pass "daemon up as $OLD_VERSION (pid $pid)"
  wait_for 300 "update to $GOOD_VERSION" healthy_as good "$GOOD_VERSION" || { fail "never updated"; journal good 60; return; }
  check "answers as $GOOD_VERSION" healthy_as good "$GOOD_VERSION"
  check "same main PID (the process handed over in place, no service restart)" [ "$(main_pid good)" = "$pid" ]
  check "systemd restarted the unit 0 times" [ "$(restarts good)" = 0 ]
  check "installed binary is $GOOD_VERSION" [ "$(binary_version good)" = "$GOOD_VERSION" ]
  check "previous binary kept as mewla.prev ($OLD_VERSION)" [ "$("$S/good/bin/mewla.prev" version --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["version"])')" = "$OLD_VERSION" ]
  check "no staged file or backup left" absent "$S/good/bin/mewla.next" "$S/good/home/.mewla/update-backup"
  check "the update came from the local test server, not GitHub" hit_local_server good
  journal good 12
}

scenario_fake() { # scenario_fake <bad|hang> <what a start does> <gate seconds>
  local name="$1" version="${FAKE_VERSION[$1]}"
  log "$name: $OLD_VERSION -> $version, a binary that cannot run as a daemon"
  old_build "$name"
  fake_binary "$S/new-$name" "$version" "$2"
  make_release "$name" "$version" "$S/new-$name"
  start_daemon "$name" "$S/old-$name"
  wait_for 90 "old daemon up" healthy_as "$name" "$OLD_VERSION" || { fail "old daemon never came up"; journal "$name"; return; }
  local pid before; pid="$(main_pid "$name")"; before="$(sha256sum "$S/$name/bin/mewla" | cut -d' ' -f1)"
  pass "daemon up as $OLD_VERSION (pid $pid)"
  wait_for 300 "rollback record" test -e "$S/$name/home/.mewla/update-failed.json" || { fail "no rollback happened"; journal "$name" 60; return; }
  wait_for 60 "old daemon back" healthy_as "$name" "$OLD_VERSION" || { fail "old daemon did not come back"; journal "$name" 60; return; }
  check "answers as $OLD_VERSION again" healthy_as "$name" "$OLD_VERSION"
  check "same main PID (never left the process)" [ "$(main_pid "$name")" = "$pid" ]
  check "systemd restarted the unit 0 times" [ "$(restarts "$name")" = 0 ]
  check "installed binary untouched" [ "$(sha256sum "$S/$name/bin/mewla" | cut -d' ' -f1)" = "$before" ]
  check "no .next, .prev or backup left" absent "$S/$name/bin/mewla.next" "$S/$name/bin/mewla.prev" "$S/$name/home/.mewla/update-backup"
  check "failed version recorded so it is not retried" grep -q "\"$version\"" "$S/$name/home/.mewla/update-failed.json"
  check "the update came from the local test server, not GitHub" hit_local_server "$name"
  journal "$name" 12
}

scenario_powercut() {
  local name=powercut version="${FAKE_VERSION[powercut]}"
  log "powercut: whole unit SIGKILLed while the new version is on trial"
  old_build "$name"
  fake_binary "$S/new-$name" "$version" 'exec sleep 600'
  make_release "$name" "$version" "$S/new-$name"
  start_daemon "$name" "$S/old-$name"
  wait_for 90 "old daemon up" healthy_as "$name" "$OLD_VERSION" || { fail "old daemon never came up"; journal "$name"; return; }
  local before; before="$(sha256sum "$S/$name/bin/mewla" | cut -d' ' -f1)"
  # Wait for the trial window: backup written, new version running as a child.
  wait_for 300 "handover in flight" test -e "$S/$name/home/.mewla/update-backup/index.json" || { fail "handover never started"; journal "$name"; return; }
  # A database the "new version" has changed in the middle of the trial.
  local db; db="$(find "$S/$name/home/.mewla" -name '*.db' -not -path '*/update-backup/*' | head -1)"
  [[ -n "$db" ]] && printf 'GARBAGE-FROM-NEW-VERSION' >> "$db"
  systemctl --user kill -s SIGKILL --kill-whom=all "$(unit_of "$name")"
  pass "SIGKILLed the whole unit mid-handover"
  wait_for 90 "old daemon back after the power cut" healthy_as "$name" "$OLD_VERSION" || { fail "did not come back"; journal "$name" 60; return; }
  check "back on $OLD_VERSION after systemd restarted the unit" healthy_as "$name" "$OLD_VERSION"
  check "installed binary untouched" [ "$(sha256sum "$S/$name/bin/mewla" | cut -d' ' -f1)" = "$before" ]
  check "interrupted handover cleaned up (.next, backup)" absent "$S/$name/bin/mewla.next" "$S/$name/home/.mewla/update-backup"
  if [[ -n "$db" ]]; then
    check "the database was restored (daemon opened it, no garbage tail)" bash -c "! tail -c 30 '$db' | grep -q GARBAGE-FROM-NEW-VERSION"
  fi
  journal "$name" 15
}

# The scenarios are independent daemons; start them together to save time.
pids=()
for scenario in "${SCENARIOS[@]}"; do
  (
    case "$scenario" in
      good) scenario_good ;;
      bad) scenario_fake bad 'echo "new version failing on purpose" >&2; exit 1' 0 ;;
      hang) scenario_fake hang 'exec sleep 600' 60 ;;
      powercut) scenario_powercut ;;
      *) echo "unknown scenario $scenario"; exit 2 ;;
    esac
  ) > "$S/out-$scenario.txt" 2>&1 &
  pids+=($!)
done
for index in "${!pids[@]}"; do
  wait "${pids[$index]}" || true
  cat "$S/out-${SCENARIOS[$index]}.txt"
  grep -q '^FAIL' "$S/out-${SCENARIOS[$index]}.txt" && FAILED=1
done
echo
if [[ $FAILED -eq 0 ]]; then echo "ALL SCENARIOS PASSED"; else echo "SOME CHECKS FAILED"; fi
exit $FAILED
