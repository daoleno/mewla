# Computer-use benchmark (Mac)

Compares Mewla's `mewla_computer` tools with agent-desktop (vercel-labs) and the Codex desktop app on the same Mac apps.
Standard-library Python 3 only; nothing here records what an app shows, only timings, sizes and counts.

## Primitives (no model in the loop)
- `python3 bench.py mewla -n 20 --out DIR --label NAME` — run inside a Mewla Worker (needs `MEWLA_WORKER_ID`). Spawns
  `mewla agent-use computer-mcp` and speaks MCP over stdio, so every call includes the MCP and control-socket hops.
- `python3 bench.py ad -n 20 --out DIR --ad /path/to/native/agent-desktop` — use the native binary, not the Node launcher.
- `python3 bench.py report DIR...` — p50/p95 table. `python3 bench.py audit [FILE]` — server-side ms from Mewla's audit file.
- `python3 bench.py tools --cmd '<mcp server command>' --out DIR` — `tools/list` of any MCP server (names, descriptions).
Run with the Mac idle; a sample where Mewla reports the agent was paused is dropped and counted. The first call is reported
as `first_ms` (cold) and p50/p95 are the warm ones. Scratch apps: TextEdit with `scratch.txt` open, Calculator, Notes,
System Settings (General), a Finder window, and Slack/Cursor with a window open.

## Task suite (the same prompt for every arm)
- `python3 tasks.py setup DIR` makes the scratch files and `DIR/expected.json` (fill the TODO entries by hand).
- `python3 tasks.py prompt ID DIR` prints a task's prompt; `check ID DIR ANSWER_FILE` judges the outcome; `reset DIR` between arms.
- agent-desktop arm: `python3 arm_ad.py DIR OUT` (claude -p, Bash only, no Mewla MCP, claude-sonnet-5-5).
- Mewla arm: spawn a Mewla Worker (claude, claude-sonnet-5-5, with the computer MCP) per task with the prompt; then
  `python3 metrics.py claude <the Worker's transcript> --since ... --until ...` for wall-clock, turns, tool calls, tokens.
- Codex arm: give the same prompt to the Codex desktop app; `python3 metrics.py codex ~/.codex/sessions/.../rollout-*.jsonl`.

## Virtual display spike
In `daemon/`: `MEWLA_VDISPLAY_SPIKE=60 MEWLA_VDISPLAY_MOVE=<pid> go test ./screen -run VirtualDisplaySpike -v` holds a
CGVirtualDisplay, moves the app's window onto it and logs pointer/front-app changes; meanwhile a Worker runs
`python3 vdisplay_probe.py <App>` to try each input path. Compare the two logs by timestamp.

Tests: `python3 tests/test_bench.py`.
