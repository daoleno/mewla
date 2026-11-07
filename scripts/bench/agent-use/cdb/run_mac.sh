#!/usr/bin/env bash
# The macOS "background" suite of the agent-use benchmark: Cua Driver Bench v2 (trycua/cua libs/cua-driver-bench, the
# suite behind the published background pass rates of cua-driver 58%, Codex Computer Use 33%, agent-desktop 20%),
# run on this Mac with Mewla's mewla_computer as one more arm.
#
# Run it INSIDE a Mewla Worker on the Mac (the daemon accepts mewla_computer calls only from one: MEWLA_WORKER_ID).
#
#   cdb/run_mac.sh [--runs 3] [--condition background|foreground|both] [--arms "cc-mewla-computer cc-agent-desktop"]
#                  [--tasks "IR-01 IR-02 IR-03 IR-04 MB-09 MB-10 MB-11"] [--preflight-only]
#
# What it does: fetch the pinned CDB commit (cdb/PIN) into a fresh scratch dir, apply our one patch (cdb/mewla-arm.patch,
# adds the arm cc-mewla-computer), build BenchLab + BenchSentinel (needs Xcode CLT and a macOS 26 SDK), run the
# runner's own preflight, run the trials, and convert results.jsonl to results/*.json with aub.py.
#
# Safety, because this is the person's own Mac and not a VM:
#  * CDB_BENCH_DISPOSABLE is NEVER set, so the runner never kills Chrome/Electron/LibreOffice/GnuCash by name.
#    Only BenchLab and BenchSentinel (its own two apps) are swept. The BenchLab tasks need nothing else.
#  * No message is sent, nothing is bought, no account is touched: BenchLab is a local test app.
#  * The screen is used for ~1 minute per trial: ONE unlock (if locked the script waits for it, never unlocks), `caffeinate` held for the whole run. Nothing else runs on
#    the screen meanwhile (one Worker on the screen at a time).
#  * The runner's own screen recorder is off (--no-recorder): it needs a private Cua Driver install with its own
#    Accessibility + Screen Recording grants, which a person would have to give. Scoring does not use the video; it uses
#    BenchSentinel and the task checkers. No recordings are kept.
#  * Preflight's Calculator open-and-kill is skipped for this arm (the person's Calculator is not ours to kill).
#  * No new Accessibility / Screen Recording grant is made by this script. Preflight names what is missing; a person
#    grants it. mewla_computer, agent-desktop and Terminal keep the grants they already have.
set -euo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
AUB=$(cd "$HERE/.." && pwd)
RUNS=3; COND=background; ARMS="cc-mewla-computer"; TASKS="IR-01 IR-02 IR-03 IR-04 MB-09 MB-10 MB-11"; PREFLIGHT_ONLY=""
while [ $# -gt 0 ]; do
  case $1 in
    --runs) RUNS=$2; shift 2;;
    --condition) COND=$2; shift 2;;
    --arms) ARMS=$2; shift 2;;
    --tasks) TASKS=$2; shift 2;;
    --preflight-only) PREFLIGHT_ONLY=1; shift;;
    *) echo "unknown flag $1" >&2; exit 2;;
  esac
done
[ "$(uname)" = Darwin ] || { echo "macOS only" >&2; exit 2; }
[ -n "${MEWLA_WORKER_ID:-}" ] || { echo "run inside a Mewla Worker (MEWLA_WORKER_ID unset)" >&2; exit 2; }
unset CDB_BENCH_DISPOSABLE
PIN=$(cat "$HERE/PIN")

export CDB_BENCH_WORK=${CDB_BENCH_WORK:-$(mktemp -d "${TMPDIR:-/tmp}/cdb-work.XXXXXX")}
# claude wherever this Mac keeps it (the runner hard-codes /opt/homebrew/bin/claude; our patch reads CDB_CLAUDE_BIN).
# The runner scrubs the environment to six variables, which drops the Worker's Claude login (trials said "Not logged in").
# Opt in to passing the auth variables (ANTHROPIC_API_KEY/AUTH_TOKEN/BASE_URL, CLAUDE_CODE_OAUTH_TOKEN, CLAUDE_CONFIG_DIR) to claude only.
export CDB_CLAUDE_PASS_AUTH_ENV=1
export CDB_CLAUDE_BIN=${CDB_CLAUDE_BIN:-$(command -v claude)}
CLAUDE=$CDB_CLAUDE_BIN
# Build against the macOS 26 SDK of the Command Line Tools when this Mac is newer than the SDK Xcode ships.
if [ -z "${SDKROOT:-}" ]; then
  for sdk in /Library/Developer/CommandLineTools/SDKs/MacOSX26*.sdk; do
    [ -d "$sdk" ] && export SDKROOT=$sdk DEVELOPER_DIR=/Library/Developer/CommandLineTools
  done
fi
# The pinned upstream tree is cached outside the scratch dir (keyed by the pin), so a flaky GitHub costs one fetch, not one
# per attempt. A fetch that stalls fails after ~40 s and is retried; it never hangs for minutes.
CACHE=${CDB_CACHE_DIR:-$HOME/.cache/mewla-agent-use-bench}/cua-$PIN
if [ ! -f "$CACHE/.ok" ]; then
  rm -rf "$CACHE" && mkdir -p "$CACHE" && cd "$CACHE"
  if [ -n "${CDB_UPSTREAM_DIR:-}" ]; then
    # A pre-fetched copy of trycua/cua at the pinned commit (must hold libs/cua-driver-bench), for a Mac that cannot reach GitHub.
    [ -d "$CDB_UPSTREAM_DIR/libs/cua-driver-bench" ] || { echo "CDB_UPSTREAM_DIR has no libs/cua-driver-bench" >&2; exit 5; }
    git init -q && cp -R "$CDB_UPSTREAM_DIR/libs" . && touch .ok
  else
    git init -q
    # Blobless partial fetch: only the commit and trees come down now, and the checkout below fetches just the blobs of
    # libs/cua-driver-bench (about 3 MB). An unfiltered fetch of this repository is 130+ MB and stalled for an hour.
    git config remote.origin.promisor true && git config remote.origin.partialclonefilter blob:none
    git sparse-checkout init --cone && git sparse-checkout set libs/cua-driver-bench
    export GIT_SSH_COMMAND="ssh -o BatchMode=yes -o ConnectTimeout=15"
    # macOS has no `timeout`: a watchdog bounds every network step to $1 seconds, whatever it is doing.
    bounded() { local secs=$1 pid w rc; shift; "$@" & pid=$!; ( sleep "$secs"; kill "$pid" 2>/dev/null ) & w=$!; rc=0; wait "$pid" || rc=$?; kill "$w" 2>/dev/null; wait "$w" 2>/dev/null || true; return $rc; }
    fetched=""
    for round in 1 2 3; do
      for url in git@github.com:trycua/cua.git https://github.com/trycua/cua.git; do
        if bounded 150 git -c http.lowSpeedLimit=2000 -c http.lowSpeedTime=40 fetch -q --depth 1 --filter=blob:none "$url" "$PIN" \
           && bounded 150 git checkout -q FETCH_HEAD; then fetched=1; break 2; fi
        echo "fetch from $url failed (round $round)" >&2
      done
      sleep $((round * 10))
    done
    [ -n "$fetched" ] || { echo "could not fetch the pinned CDB commit from GitHub over SSH or HTTPS: nothing was run (set CDB_UPSTREAM_DIR to a pre-fetched copy)" >&2; rm -rf "$CACHE"; exit 5; }
    touch .ok
  fi
fi
# A fresh copy to patch: the cache itself stays pristine.
SRC="$CDB_BENCH_WORK/cua"
rm -rf "$SRC" && mkdir -p "$SRC" && cp -R "$CACHE/." "$SRC/" && cd "$SRC"
git apply "$HERE/mewla-arm.patch"
MB="$SRC/libs/cua-driver-bench/automated-eval/macos_bench"
mkdir -p "$CDB_BENCH_WORK/src" "$CDB_BENCH_WORK/build"
rm -rf "$CDB_BENCH_WORK/src/macos_bench" && cp -R "$MB" "$CDB_BENCH_WORK/src/macos_bench"
cp -R "$SRC/libs/cua-driver-bench/taskpack" "$CDB_BENCH_WORK/taskpack"
(cd "$CDB_BENCH_WORK/src/macos_bench" && swift/build.sh "$CDB_BENCH_WORK/build")

cd "$CDB_BENCH_WORK/src/macos_bench"
# The runner's preflight pins Claude Code 2.1.289, macOS 26.5.2 (25F84) for every arm: the registered VM. This Mac is
# not that VM, so write a pins file with what is installed here, and say so in the rows (versions are recorded per row).
PINS="$CDB_BENCH_WORK/pins.mac.json"
python3 -I - pins.json "$PINS" "$(claude --version)" "$(sw_vers -productVersion)" "$(sw_vers -buildVersion)" <<'PY'
import json, sys
src, dst, claude, mac, build = sys.argv[1:6]
p = json.load(open(src))
p.update({"claude_code_version": claude, "macos_version": mac, "macos_build": build,
          "environment": "the person's own Mac, not the registered Lume VM (observed versions substituted for the pins)"})
json.dump(p, open(dst, "w"), indent=1)
PY
export CDB_MEWLA_BIN=${CDB_MEWLA_BIN:-$(command -v mewla)}
# Keep the display awake for this script's lifetime only (memory mac-unlock-batching).
caffeinate -d -i -w $$ &
# Everything from here needs the screen. Locked: wait for the person's one unlock (never try to unlock it), up to
# CDB_WAIT_UNLOCK_MIN minutes, then go straight on in the same batch.
# Capture first, match after: `ioreg | grep -q` quits at the first hit and, under pipefail, the SIGPIPE'd ioreg makes the
# whole pipeline fail, which read as "not locked" and skipped the wait.
locked() {
  local out
  out=$(ioreg -n Root -d1 2>/dev/null) || true
  [[ $out == *'"CGSSessionScreenIsLocked" = Yes'* || $out == *'"IOConsoleLocked" = Yes'* ]]
}
WAITED=0
while locked; do
  [ "$WAITED" -ge $(( ${CDB_WAIT_UNLOCK_MIN:-45} * 60 )) ] && { echo "the screen stayed locked for ${CDB_WAIT_UNLOCK_MIN:-45} min: nothing was run" >&2; exit 4; }
  [ "$WAITED" -eq 0 ] && echo "screen is locked: waiting for the person to unlock it (polling every 20 s)" >&2
  sleep 20; WAITED=$((WAITED + 20))
done
# Flags every runner call shares. --tasks keeps the preflight and the run to the BenchLab tasks (the pack tasks want
# LibreOffice, GnuCash, Electron apps and CDB_BENCH_DISPOSABLE, which this Mac never gets). --no-model-preflight skips the
# three Haiku probes (quota reading; this run is small). Disk floor 10 GB: no recordings are kept.
COMMON=(--bench-v2 --pins "$PINS" --no-recorder --allow-non-terminal --claude-bin "$CLAUDE" --disk-floor-gb 10 --build-dir "$CDB_BENCH_WORK/build")
bg_tasks() { for t in $TASKS; do case $t in MB-10|MB-11) ;; *) printf '%s ' "$t";; esac; done; }
first=$( [ "$COND" = foreground ] && echo foreground || echo background )
if [ "$first" = background ]; then PT=$(bg_tasks); else PT=$TASKS; fi
python3 run_bench.py preflight "${COMMON[@]}" --no-model-preflight --condition "$first" --arms $ARMS --tasks $PT || { echo "preflight failed: fix what it names, then rerun" >&2; exit 3; }
[ -n "$PREFLIGHT_ONLY" ] && exit 0

for cond in $( [ "$COND" = both ] && echo "background foreground" || echo "$COND" ); do
  RID="aub-$(date +%Y%m%d-%H%M)-$cond"
  if [ "$cond" = background ]; then T=$(bg_tasks); else T=$TASKS; fi
  python3 run_bench.py run "${COMMON[@]}" --no-model-preflight --condition "$cond" --park-pointer --arms $ARMS --tasks $T \
    --phase1-runs "$RUNS" --phase2-runs 0 --run-id "$RID"
  python3 -I "$AUB/aub.py" import-cdb "$CDB_BENCH_WORK/runs/$RID"
done
echo "results are in $AUB/results; raw runs stay in $CDB_BENCH_WORK/runs, outside git"
