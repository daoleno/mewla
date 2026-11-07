# Agent-use benchmark

One repeatable way to judge `mewla_chrome`, `mewla_computer` (and later `mewla_phone`, `mewla_iphone`) by numbers, with
metrics people already recognise. The reasoning, the survey of public benchmarks and the limits are in
`~/.mewla/brain/workspace/worklog/agent-use-benchmark-1107/DESIGN.md`; this file is how to run it.

**The score** is `full pass`: the task's checker passed, the agent stayed GUI-only, and the person was not disturbed.
For a foreground run the last part is not asked. Pass rate, background pass rate, disturbances, wall time, steps and
tokens are computed by `aub.py report` from the trial rows in `results/*.json` (schema `aub/1`); nothing is stored twice.

| Suite | Tool layers | Where | Source |
|---|---|---|---|
| `web-fixture` (14 tasks, checker reads the fixture's own event log) + a person typing in another window | `mewla_chrome` (real extension, native host, bridge), `agent-browser` for comparison | this Linux box | `../browser/` + `web/` |
| `cdb-macos-benchlab` (Cua Driver Bench v2: IR-01..04, MB-09 background; MB-10/11 foreground) | `mewla_computer`, `agent-desktop`; cua-driver, Codex CU, browser-use as published numbers | the Mac | `cdb/`, upstream [trycua/cua](https://github.com/trycua/cua/tree/main/libs/cua-driver-bench) (FSL-1.1-MIT, pinned in `cdb/PIN`, not vendored) |

The model is fixed (`claude-sonnet-5-5`, written into every file) so the numbers move with the tool layer, not the model.

## Run

```bash
# Web, on this computer: own Xvnc display, own Chrome for Testing, throwaway state; the live daemon and the user's
# Chrome are never touched. ~30 min for 3 reps of 14 tasks on both sides.
scripts/bench/agent-use/web/run.sh --reps 3 --with-long
# Same, on a display with a real window manager (kwin_x11; a bare X server is not a desktop), tagged for before/after:
scripts/bench/agent-use/web/run.sh --reps 3 --with-long --sides mewla --wm kwin --tag after-my-change
# Extra checks of what a task needs from the page being drawn (a window kept out of the way): --only shot,raf

# macOS, inside a Mewla Worker on the Mac (one unlock, caffeinate inside the script). Ask the Mac with:
#   mewla computers send mac -kind request -user-said "<the user's words>" "<what to run: see cdb/run_mac.sh>"
scripts/bench/agent-use/cdb/run_mac.sh --preflight-only
scripts/bench/agent-use/cdb/run_mac.sh --condition both --runs 3

# Published comparison rows (Scale's per-trial dataset; counts only):
python3 scripts/bench/agent-use/aub.py published            # or --rows saved.json if the server rate-limits

# Look:
python3 scripts/bench/agent-use/aub.py report                # every file in results/, published rows alongside
python3 scripts/bench/agent-use/aub.py trend --suite web-fixture --tool mewla_chrome
python3 scripts/bench/agent-use/aub.py check results/*.json
```

## Non-interference

- **macOS** (CDB's own, so it compares with public numbers): keyboard focus lost, window raised over the user's,
  typing lost/redirected, real pointer moved >1 px, clipboard changed, Space switched, dialog on the user's app, user's app
  (de)activated. A criterion that was not measured counts as a disturbance.
- **Linux web** (`web/xuser.c`, ours): a simulated person types one character a second into a Chrome window of its own
  with XTest; it samples X input focus, the topmost window and the real pointer 20 times a second, and the page
  logs the keys and visibility it saw. Measured: key focus lost, window raised over the user's, typing lost, pointer
  moved, user's page hidden; also `covered_ms` (how long another window was over theirs; information only, not part of full pass). Not measured: clipboard, desktop switch, dialog on the user's app, app activation. Every
  row carries both lists. Not comparable with CDB's numbers.

## Files

- `aub.py` result format, `report`, `trend`, `published`, importers.
- `web/run.sh`, `web/xuser.c`: the web run. `../browser/suite.mjs --nonint` runs the tasks and the probe;
  `daemon/agentuse/benchbridge_test.go` (`TestAgentUseBenchBridge`) brings up the real chain and prints how to reach it.
- `cdb/run_mac.sh`, `cdb/PIN`, `cdb/mewla-arm.patch`: the Mac run. The patch adds one arm (`cc-mewla-computer`:
  `mewla agent-use computer-mcp`, tool classes for `mewla_computer`) to the pinned upstream runner; it is applied to a
  scratch checkout, never committed upstream or here as a copy of their code.
- `results/`: one JSON per run, `published-cdb-scale.json` for Scale's numbers. Raw recordings and transcripts stay out of git.

## Rules this keeps

No message is sent, nothing bought, no real account touched (fixture pages and a local test app only). Nothing here
signs in to anything. The Mac runner never sets `CDB_BENCH_DISPOSABLE`. Scratch dirs are fresh `mktemp -d`.
