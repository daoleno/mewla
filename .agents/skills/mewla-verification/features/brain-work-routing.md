# Brain work routing

Brain is the canonical owner of current work, executor routing, and discoverable playbooks. This feature verifies that a Worker can read that contract from the live daemon without changing it.

## Sub-features

- `brain-context` returns the host and delegated executor identities.
- `brain-playbooks` returns the discoverable Brain workflow catalog.
- `brain-privacy` emits a compact report instead of persisting raw current work.

## How to get to it (user POV)

- Run `scripts/verify-mewla-orchestration.sh --json --state-dir /absolute/path/to/existing-mewla-state` from the repository root.
- Read the `brain_context` and `brain_playbooks` entries in the report.

## Driving it with the Mewla CLI

Preconditions:

- The exact Mewla daemon is running and passes `mewla doctor --json`.
- The checkout contains the source and test anchors in `features/manifest.json`.

- **Context.** Run the lever. The report contains a passing `brain_context` runtime check, a separate runtime daemon identity, a host executor ID, a delegated executor ID, and a Worker count.
- **Playbooks.** Read the same report. The report contains a passing `brain_playbooks` preflight and the `delegate-brief` catalog entry. This does not prove Skill loading.
- **Privacy.** Inspect the report. It contains no raw `current`, transcript, or Work objective field.

## Gotchas

- A healthy daemon with stale source is not a pass. The source and test checks must also pass.
- The report does not prove provider quality or a mobile or desktop client flow.
- A missing daemon is a prerequisite failure. Do not start a second server as part of this bounded preflight.

## Host continuity

The source map includes confirmed tmux absence, serialized Host launch, native
Claude process identity, and exclusive Brain-root ownership. The CLI preflight
is read-only evidence of the current binding. After a continuity repair, also
compare `host_worker.id` and `observed` with the unchanged live tmux pane/process,
check Worker identities before and after restart, and inspect the replacement
audit for recovery with no new launch. An App-to-Host message requires separate
explicitly authorized device verification; the preflight does not send input.

Host continuity also requires the immutable model-proxy URL held by the live
provider. Check that exact endpoint after daemon restart, not only the current
listener metadata. Linux startup restores known routes' live Claude endpoints
on the same Router; the regression changes listener metadata and verifies both
old and new ports serve the route across restarts and close without leaking.

Executor switches preserve the Brain thread while changing its Host Session.
Transcript resolution must compare the complete saved binding before writeback
and reject a departing provider's observation. The regression fixtures cover
both Claude/Codex directions, stale in-flight resolutions, and socket snapshots
plus subsequent deltas after repairing a cross-provider transcript binding.
The App hook test also checks input targets, subscription cleanup, Activity,
assistant replies, errors, and late old-subscription frames. These are local
behavioral tests, not evidence of a real provider call or native device UI.

Review delivery preserves the explicit Claude resume/session identity from the
live provider process. The failure regressions reject repeated immediate claims,
deliver a later review while the older one backs off, and retain retry exhaustion
across restart. Context exposes `current_work[].review_delivery` for failures,
including attempts, reason, retry time and exhaustion. A CLI preflight alone does
not prove delivery: inspect exact canonical `review.delivered` identities and the
provider transcript input after the existing supervisor reloads the daemon.

Claude Code can wrap a large terminal paste in a whole `pasted_content` envelope.
Admission keeps the raw digest and an exact inner digest only for the recognized
fixed envelope with matching IDs. Confirmation and pending recovery retain all
identity, cursor and time fences; malformed, nested, prefixed or suffixed forms
cannot match an inner payload. Sweep retains a review handler while its exact
admission is live, so expiry cannot erase the recovery capability. Runtime proof
requires a real wrapped native user event correlated to the canonical admitted
digest and `review.delivered`; the CLI preflight does not send such an event.
