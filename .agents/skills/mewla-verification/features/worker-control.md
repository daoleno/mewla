# Worker control

Mewla exposes visible Worker identities through the canonical control socket. This feature verifies the read path that a Brain or Worker uses to inspect lifecycle ownership.

## Sub-features

- `worker-list` returns immutable tmux `%pane_id` Worker IDs and statuses.
- `worker-ownership` exposes delegated ownership without selecting a provider.
- `worker-count` reports the observed list size without saving session content.

## How to get to it (user POV)

- Run `scripts/verify-mewla-orchestration.sh --json --state-dir /absolute/path/to/existing-mewla-state` from the repository root.
- Read the `worker_list` entry and its `runtime.worker_count` value.

## Driving it with the Mewla CLI

Preconditions:

- `mewla doctor --json` passes for the exact daemon state.
- The control socket is owned by that daemon.

- **List.** Run the lever. The report contains `worker_list: pass` and a numeric Worker count.
- **Identity.** Inspect the runtime summary. Worker IDs are counted from the canonical response and are not recreated from process names.
- **Failure.** Point `--state-dir` at a missing or unrelated state directory. The lever exits nonzero and reports the failed prerequisite instead of claiming a pass.

## Gotchas

- A Worker status is not a completion decision. Brain Work and Event state remain authoritative.
- A startup proof must include the exact Session's pane capture after at least
  two minutes; transport acceptance alone does not establish provider execution.
  `daemon/cmd/mewla/spawn_recovery_test.go` covers vanished startup panes and
  same-Work recovery, and `daemon/brain/absent_preparation_test.go` covers the
  retirement fence. `daemon/work/delegated_trust_test.go` checks native cwd trust.
- Do not use `pkill`, `killall`, or process-name matching to clean up a verification run.
- An empty list can be valid. The control path must pass even when no Worker is active.

## DSH and service lifetime evidence

The source map includes the additive DSH executor, exact native Session bridge,
Zstandard event projection and Services tunnel process owner. `worker_list` proves
only the canonical inventory read, not these mutations or provider output.

Use `daemon/work/dsh_conversation_test.go` for source identity, packed streaming,
tool correlation, attachment confinement and exclusive-owner regressions. A bounded
native DSH smoke must use private state and record the exact Session identity;
never infer success from a launch option alone or change saved model choices.

`daemon/watcher/service_tunnels_test.go` checks HTTP/WebSocket proxy behavior,
origin/listener replacement, stop isolation and Linux parent-death cleanup. Its
public fixture test is explicit opt-in and always stops its own tunnel. A URL or
native Cloudflare connection confirmation alone is not public reachability proof.

## Machine telemetry and ownership cleanup

`mewla resources --json` reads the cached machine snapshot and chart history.
The authenticated API contract is in `docs/resource-telemetry.md`. Threshold
transitions use the durable Brain Work Event lane; they do not authorize daemon
resource intervention. `mewla worker release -id SESSION -pid PID -start START`
releases a selected exact tool tree while retaining its provider.

`resource_cleanup_real_test.go` launches a plain inert tmux Worker, re-observes
it, and verifies that close removes its detached descendant.
`resource_cost_linux_test.go` provides the opt-in sampler CPU/IO budget check.
`resource_pressure_test.go` covers sustained transitions, hysteresis and cooldown;
Brain and server tests cover the event envelope and authenticated API. The lever's
Worker inventory read alone does not prove these mutation or pressure flows.

## Pane ownership and restart migration

Worker identity is the owned `%pane_id` on the canonical server. A window is
presentation metadata; changing focus or splitting a window cannot change its
Worker. Ownership markers, input receipts, provider capture and process probes
belong to that pane. Closing it removes only that pane.

`daemon/watcher/pane_identity_real_test.go` reproduces the split/focus delivery
case on an isolated socket, then proves removal refuses input, pane-local launch
environment does not leak to a user split, and migration never selects focus.
`daemon/brain/pane_migration_test.go` reopens a running canonical turn twice after
migration, including provider route bindings and Telegram reply/deduplication
references. `daemon/terminal/tmux_integration_test.go` covers resolving a Worker
pane to its display window and refusing a removed pane. These inert fixtures
provide mutation evidence; `worker_list` alone does not.

Legacy `session:window_id` references are resolved once during startup using
recorded pane-generation evidence (or an unambiguous single-pane window). The
alias journal commits before metadata and state migration, so an interrupted
load resumes the same binding. An ambiguous split fails migration instead of
adopting its active pane. Old launch shells may report their legacy ID through
the control API only while the pinned pane retains that exact legacy marker.
