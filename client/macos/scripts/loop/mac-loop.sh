#!/usr/bin/env bash
# A warm Mac for one task, from this (Linux) computer: .github/workflows/macos-loop.yml on a GitHub macOS runner.
#
#   mac-loop.sh start            push a per-run public key to mac-loop/run, wait for the runner, write the ssh config
#   mac-loop.sh ui|host|daemon   sync this checkout, rebuild that part on the runner, relaunch Mewla Dev, screenshot
#   mac-loop.sh launch           relaunch Mewla Dev as it is, then a screenshot
#   mac-loop.sh shot [name]      screenshot of the screen into $MAC_LOOP_DIR/shots/<name>.png
#   mac-loop.sh log [n]          the last n lines of the app's and its daemon's logs
#   mac-loop.sh click X Y | type TEXT | key CODE   drive the UI (CGEvent click, System Events keystroke)
#   mac-loop.sh ssh [cmd…]       a shell (or a command) on the runner, in the job's GUI session
#   mac-loop.sh stop             end the hold step (the run goes green) and forget the key
#
# State (the private key, the ssh config) lives in $MAC_LOOP_DIR (default: $TMPDIR/mac-loop); the key never leaves it.
# Needs gh (the daoleno account), cloudflared, rsync and ssh here.
set -euo pipefail

root="$(cd "$(dirname "$0")/../../../.." && pwd)"
dir="${MAC_LOOP_DIR:-${TMPDIR:-/tmp}/mac-loop}"
repo=daoleno/mewla
branch=mac-loop/run
gh_() { GH_TOKEN="$(gh auth token -u daoleno)" gh "$@"; }
now() { date +%s.%N; }
stamp() { printf '%-26s %6.1f s\n' "$1" "$(echo "$(now) - $2" | bc)"; }
sshc() { ssh -F "$dir/ssh_config" mac "$@"; }

cmd="${1:-}"; shift || true
case "$cmd" in
start)
  mkdir -p "$dir"; chmod 700 "$dir"
  t0=$(now)
  rm -f "$dir/key" "$dir/key.pub"
  ssh-keygen -q -t ed25519 -N '' -C mac-loop -f "$dir/key"
  # The commit: this checkout's HEAD plus the public key, made without touching the checkout or its index.
  cd "$root"
  blob=$(git hash-object -w "$dir/key.pub")
  idx="$dir/index"; rm -f "$idx"
  GIT_INDEX_FILE="$idx" git read-tree HEAD
  GIT_INDEX_FILE="$idx" git update-index --add --cacheinfo "100644,$blob,.github/mac-loop/authorized_key"
  tree=$(GIT_INDEX_FILE="$idx" git write-tree); rm -f "$idx"
  commit=$(git commit-tree "$tree" -p HEAD -m "mac-loop: a run's public key")
  GH_TOKEN="$(gh auth token -u daoleno)" git push -q -f "https://github.com/$repo.git" "$commit:refs/heads/$branch"
  stamp pushed "$t0"
  run=""
  while [[ -z "$run" ]]; do
    sleep 3
    run=$(gh_ api "repos/$repo/actions/runs?branch=$branch&head_sha=$commit" --jq '.workflow_runs[0].id // empty')
  done
  echo "$run" > "$dir/run"; stamp "run $run" "$t0"
  started=""
  while :; do
    st=$(gh_ api "repos/$repo/actions/runs/$run/jobs" --jq '.jobs[0] | "\(.status) \(.started_at // "")"')
    [[ "$st" == in_progress* && -z "$started" ]] && { started=1; stamp "runner picked up" "$t0"; }
    [[ "$st" == completed* ]] && { echo "the run ended before the way in opened: $st"; exit 1; }
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
  sshc "screencapture -x ~/mac-loop/out/$name.png"
  scp -q -F "$dir/ssh_config" "mac:mac-loop/out/$name.png" "$dir/shots/$name.png"
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
