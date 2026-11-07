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
#  * The screen is used for ~1 minute per trial: one unlock, `caffeinate` held for the whole run. Nothing else runs on
#    the screen meanwhile (one Worker on the screen at a time).
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
SRC="$CDB_BENCH_WORK/cua"
mkdir -p "$SRC" && cd "$SRC"
if [ ! -d .git ]; then
  git init -q && git remote add origin https://github.com/trycua/cua.git
  git sparse-checkout init --cone && git sparse-checkout set libs/cua-driver-bench
  git fetch -q --depth 1 origin "$PIN" && git checkout -q FETCH_HEAD
  git apply "$HERE/mewla-arm.patch"
fi
MB="$SRC/libs/cua-driver-bench/automated-eval/macos_bench"
mkdir -p "$CDB_BENCH_WORK/src" "$CDB_BENCH_WORK/build"
rm -rf "$CDB_BENCH_WORK/src/macos_bench" && cp -R "$MB" "$CDB_BENCH_WORK/src/macos_bench"
cp -R "$SRC/libs/cua-driver-bench/taskpack" "$CDB_BENCH_WORK/taskpack"
(cd "$CDB_BENCH_WORK/src/macos_bench" && swift/build.sh "$CDB_BENCH_WORK/build")

cd "$CDB_BENCH_WORK/src/macos_bench"
export CDB_MEWLA_BIN=${CDB_MEWLA_BIN:-$(command -v mewla)}
# Keep the display awake for this script's lifetime only (memory mac-unlock-batching).
caffeinate -d -i -w $$ &
python3 run_bench.py preflight --bench-v2 --build-dir "$CDB_BENCH_WORK/build" --arms $ARMS || { echo "preflight failed: fix what it names, then rerun" >&2; exit 3; }
[ -n "$PREFLIGHT_ONLY" ] && exit 0

for cond in $( [ "$COND" = both ] && echo "background foreground" || echo "$COND" ); do
  RID="aub-$(date +%Y%m%d-%H%M)-$cond"
  python3 run_bench.py run --bench-v2 --condition "$cond" --park-pointer --arms $ARMS --tasks $TASKS \
    --phase1-runs "$RUNS" --phase2-runs 0 --run-id "$RID" --build-dir "$CDB_BENCH_WORK/build"
  python3 -I "$AUB/aub.py" import-cdb "$CDB_BENCH_WORK/runs/$RID"
done
echo "results are in $AUB/results; raw runs (with recordings) stay in $CDB_BENCH_WORK/runs, outside git"
