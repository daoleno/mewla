#!/usr/bin/env bash
# A warm Mac for one task, from this (Linux) computer: .github/workflows/macos-loop.yml on a GitHub macOS runner.
#
#   mac-loop.sh start [--host] [--minutes N]
#                                refuse beside a release or another loop; else dispatch the workflow with a per-run
#                                public key, wait for the runner, write the ssh config (--host: build the host's loop
#                                profile in the background once warm, for Rust host changes; --minutes: default 45)
#   mac-loop.sh ui|host|daemon   sync this checkout, rebuild that part on the runner, relaunch Mewla Dev, screenshot
#   mac-loop.sh launch           relaunch Mewla Dev as it is, then a screenshot
#   mac-loop.sh shot [name]      screenshot of the screen into $MAC_LOOP_DIR/shots/<name>.png
#   mac-loop.sh log [n]          the last n lines of the app's and its daemon's logs
#   mac-loop.sh click X Y | type TEXT | key CODE   drive the UI (CGEvent click, System Events keystroke)
#   mac-loop.sh ssh [cmd…]       a shell (or a command) on the runner, in the job's GUI session
#   mac-loop.sh stop             end the hold step (the run goes green) and forget the key
#
# State (the private key, the ssh config) lives in $MAC_LOOP_DIR (default: $TMPDIR/mac-loop); the key never leaves it.
# Needs gh (the daoleno account), cloudflared, rsync and ssh here. The rules: README.md beside this script.
set -euo pipefail

root="$(cd "$(dirname "$0")/../../../.." && pwd)"
dir="${MAC_LOOP_DIR:-${TMPDIR:-/tmp}/mac-loop}"
repo=daoleno/mewla
ref="${MAC_LOOP_REF:-main}"
gh_() { GH_TOKEN="$(gh auth token -u daoleno)" gh "$@"; }
now() { date +%s.%N; }
stamp() { printf '%-26s %6.1f s\n' "$1" "$(echo "$(now) - $2" | bc)"; }
# A session on the runner; when it fails, the run may have yielded to a waiting macOS job or run out of time.
sshc() {
  ssh -F "$dir/ssh_config" mac "$@" || { local rc=$?; echo "(no Mac: has run $(cat "$dir/run" 2>/dev/null) yielded or ended? gh run view it)" >&2; return $rc; }
}

cmd="${1:-}"; shift || true
case "$cmd" in
start)
  host_build=false; minutes=45
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --host) host_build=true; shift ;;
      --minutes) minutes="${2:?}"; shift 2 ;;
      *) echo "start: unknown argument $1" >&2; exit 2 ;;
    esac
  done
  # Never beside a release or another loop (the hold itself leaves when another macOS job waits).
  blocked=$(GITHUB_REPOSITORY=$repo GH_TOKEN="$(gh auth token -u daoleno)" python3 -I "$(dirname "$0")/yield.py" blockers)
  [[ -z "$blocked" ]] || { echo "not now:"; echo "$blocked"; exit 1; }
  mkdir -p "$dir"; chmod 700 "$dir"
  t0=$(now)
  rm -f "$dir/key" "$dir/key.pub"
  ssh-keygen -q -t ed25519 -N '' -C mac-loop -f "$dir/key"
  since=$(date -u +%Y-%m-%dT%H:%M:%SZ -d '-5 seconds')
  gh_ workflow run macos-loop.yml --repo "$repo" --ref "$ref" -f pubkey="$(cat "$dir/key.pub")" -f minutes="$minutes" -f host="$host_build"
  stamp dispatched "$t0"
  run=""
  while [[ -z "$run" ]]; do
    sleep 3
    run=$(gh_ api "repos/$repo/actions/workflows/macos-loop.yml/runs?event=workflow_dispatch&created=>=$since" --jq '.workflow_runs[0].id // empty')
  done
  echo "$run" > "$dir/run"; stamp "run $run" "$t0"
  started=""
  while :; do
    st=$(gh_ api "repos/$repo/actions/runs/$run/jobs" --jq '.jobs[0] | "\(.status) \(.started_at // "")"')
    [[ "$st" == in_progress* && -z "$started" ]] && { started=1; stamp "runner picked up" "$t0"; }
    [[ "$st" == completed* ]] && { echo "the run ended before the way in opened ($st); its summary says why: gh run view $run --repo $repo"; exit 1; }
    art=$(gh_ api "repos/$repo/actions/runs/$run/artifacts" --jq ".artifacts[] | select(.name == \"mac-loop-meet-$run\") | .id")
    [[ -n "$art" ]] && break
    sleep 10
  done
  rm -rf "$dir/meet"; mkdir -p "$dir/meet"
  gh_ api "repos/$repo/actions/artifacts/$art/zip" > "$dir/meet.zip"
  python3 -I -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$dir/meet.zip" "$dir/meet"
  host=$(cat "$dir/meet/host")
  echo "mac $(cat "$dir/meet/host_key.pub")" > "$dir/known_hosts"
  cat > "$dir/ssh_config" <<EOF
Host mac
  HostName $host
  User $(cat "$dir/meet/user")
  IdentityFile $dir/key
  IdentitiesOnly yes
  ProxyCommand cloudflared access ssh --hostname %h
  HostKeyAlias mac
  UserKnownHostsFile $dir/known_hosts
  StrictHostKeyChecking yes
  ControlMaster auto
  ControlPath $dir/cm-%C
  ControlPersist 4h
  ServerAliveInterval 30
  LogLevel ERROR
EOF
  sshc 'echo "on $(hostname), $(sw_vers -productVersion), user $USER"'
  stamp "first ssh" "$t0"
  ;;
ssh) sshc "$@" ;;
sync)
  t=$(now)
  ws=$(sshc 'cat ~/mac-loop/workspace')
  rsync -az --delete -e "ssh -F $dir/ssh_config" \
    --exclude node_modules --exclude .nsh --exclude target --exclude dist --exclude /daemon/webui/dist \
    "$root/client" "$root/daemon" "mac:$ws/"
  stamp sync "$t"
  ;;
ui|host|daemon|launch)
  t0=$(now)
  [[ "$cmd" == launch ]] || "$0" sync
  sshc "bash \"\$(cat ~/mac-loop/workspace)/client/macos/scripts/loop/remote.sh\" $cmd"
  "$0" shot "${1:-$cmd}"
  stamp "$cmd: edit to screenshot" "$t0"
  ;;
shot)
  t=$(now); name="${1:-shot}"; mkdir -p "$dir/shots"
  # The job's sshd has no sftp subsystem: the file comes back on the session's stdout.
  sshc "screencapture -x ~/mac-loop/out/$name.png && cat ~/mac-loop/out/$name.png" > "$dir/shots/$name.png"
  stamp "shot $dir/shots/$name.png" "$t"
  ;;
log) sshc "tail -n ${1:-40} ~/mac-loop/run/data/logs/* ~/mac-loop/run/state/logs/app-daemon.log 2>/dev/null" ;;
click) sshc "~/mac-loop/click $1 $2" ;;
type) sshc "osascript -e 'tell application \"System Events\" to keystroke \"$1\"'" ;;
key) sshc "osascript -e 'tell application \"System Events\" to key code $1'" ;;
stop)
  sshc 'touch ~/mac-loop/stop' || true
  ssh -F "$dir/ssh_config" -O exit mac 2>/dev/null || true
  rm -f "$dir/key" "$dir/key.pub"
  echo "stopped; run $(cat "$dir/run" 2>/dev/null) ends at its next check"
  ;;
*) sed -n 2,14p "$0"; exit 2 ;;
esac
