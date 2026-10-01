# Executors

Zen launches and observes AI CLIs through tmux. Configuration is optional: if `~/.zen/executors.toml` is missing, built-in defaults apply.

**One authenticated executor on `PATH` is enough** for a usable Zen installation. You do not need Codex, Claude, Cursor, Grok, Pi, and OpenCode all installed.

## Built-in defaults (current code)

When the file is absent, zen uses approximately:

| Name | Default command | Notes |
| --- | --- | --- |
| `codex` | `codex` | Default **delegated** executor for Brain |
| `claude` | `claude` | |
| `agent` | `cursor-agent --force --sandbox disabled` | Permission / sandbox bypass |
| `grok` | `grok --no-alt-screen --permission-mode bypassPermissions` | Permission bypass |
| `pi` | `pi` | Permissive by default; Zen injects absolute `--session` |
| `opencode` | `opencode` | Interactive safe default; Brain/Calendar may derive `--auto` |

These defaults are **not** changed by shipping [executors.example.toml](../executors.example.toml). The example file documents safer vs autonomous profiles for you to copy.

## Install a profile

```bash
cp executors.example.toml ~/.zen/executors.toml
# edit executor definitions/commands, then restart zen so the catalog reloads
```

`delegated_executor` names which executor Brain uses for delegated work and ordinary default session creation. Only that CLI needs to be installed if you only use Brain delegation plus that tool.

Switch the live Delegated Executor without restarting the daemon:

```bash
zen brain set-delegated grok
```

That validates the id against the loaded catalog, persists `delegated_executor` atomically, and updates every future launch/read boundary in the same process. Existing Worker sessions keep their original executor. Editing executor definitions or commands still requires a restart so the static catalog reloads.

## Default delegated Worker configuration

Use one atomic operation against the running daemon:

```sh
zen worker defaults -executor codex -model gpt-6-astra -reasoning medium
zen worker defaults --json
```

The response includes `executor`, `model`, `reasoning`, and the actual resolved
`command`. The existing `ExecutorConfig` owns all three values in
`~/.zen/executors.toml` (`delegated_executor`, `delegated_model`,
`delegated_reasoning`). No Provider model or native Codex configuration is
rewritten. Persistence succeeds before live state changes; a failed save leaves
all three prior values effective. The next delegated spawn uses the new snapshot
without a service restart. Already running Sessions are unchanged.

The model/effort fields are Worker-only. The executor catalog command remains
the host/manual command, so changing Worker defaults cannot rewrite Brain's
model. Native Codex config (including `$CODEX_HOME/config.toml`), selected
profiles and resumed thread settings are lower priority than explicit Worker
launch options. No duplicate model/effort options are appended. Precedence is:

1. Single-launch `-model` / `-reasoning` values.
2. Model/effort explicitly present in `-command` (including `codex resume ID`).
3. Saved Worker model/effort, when launching the selected default client.
4. Catalog command, then the native client's configuration when omitted.

Setting requires all three flags; pass an empty model or reasoning to clear that
override. `zen brain set-delegated CLIENT` remains the executor-only operation:
changing clients clears Worker model/effort overrides rather than carrying a
Codex model to another client. The startup `ZEN_DELEGATED_EXECUTOR` lock is
respected. The adapter currently accepts model/reasoning selection for Codex;
other clients can be selected with empty values and keep native selection.

Claude, Cursor and Grok delegated launches reuse the existing unattended client
adapters. Claude `--permission-mode auto`, `dontAsk`, and `acceptEdits` are not
bypass guarantees and are rejected for delegated work; the manual/Brain host
paths keep their prior permission policy. Pi/DSH retain their native client
behavior; extensions and authentication may still require setup.

All delegated Codex launches, including explicit commands and resume, use the
same client adapter and run autonomously by default. Explicit interactive
approval/sandbox flags fail with an explanation instead of silently bypassing
policy or adding conflicting flags. Shell composition is not a supported Codex
launch command. Ordinary manual/hidden Sessions retain their own policies.

Delegated launches prepare folder trust before starting the provider. Codex's
native `projects."<cwd>".trust_level = "trusted"` is persisted in its effective
`CODEX_HOME/config.toml`; an invocation `-c` override alone does not clear the
0.159.2 folder gate. Zen also passes `--dangerously-bypass-hook-trust` and
`-c check_for_update_on_startup=false`. The latter prevents a startup update
picker from consuming the brief's Enter and exiting into npm. Claude's effective
`.claude.json` gets `projects[<cwd>].hasTrustDialogAccepted = true`, independently
of `bypassPermissions`. Existing unrelated settings are retained. Trust applies
to the actual cwd and its resolved symlink spelling, not every parent directory.
Cursor already supplies `--trust --approve-mcps` with its unattended execution
flags, and delegated launch disables its background self-updater with the
native `--disable-auto-update` option; OpenCode uses `--auto`. Initial input still waits for the provider's
composer, and update menus are explicitly excluded from readiness.

Claude 2.1.285 wraps bracketed terminal paste in `<pasted_content>`. For delegated
input, Zen types the short instruction `Execute: ` outside that wrapper before
pasting the unchanged brief. This makes the assignment explicit in the same
user turn, without a second prompt or a permission-policy override. Manual
Session input retains native paste semantics.

`worker spawn -work ID` automatically retires an old ordinary delegated
preparation when an authoritative inventory proves its Session absent. Unknown
server reachability retains the fence. A retired preparation records unknown
delivery and rejects late signals; it does not claim successful execution or
definite non-submission. The normal inventory pass and explicit Session close
use the same retirement boundary. Accepted/ambiguous and Host/review admissions
keep their existing handling rules. A final presence check prevents spawn from
reporting a fabricated running Worker when its pane disappeared during handoff.

Control requests: `worker_defaults_get`, `worker_defaults_set` with `executor`,
`model_id`, `reasoning_effort`. Authenticated WebSocket equivalents are
`get_worker_defaults` / `set_worker_defaults` using `executor_id`, `model_id`,
`reasoning_effort`. Both call the same owner; no UI-specific default store exists.
Brain should perform routine configuration directly, then read back the result.

## Permission bypass risks

Flags such as:

- `cursor-agent --force --sandbox disabled`
- `grok ... --permission-mode bypassPermissions`
- Codex `--dangerously-bypass-approvals-and-sandbox` (appended for **Brain-delegated** Codex sessions in current code)
- OpenCode `--auto` (derived only for autonomous Brain/Calendar OpenCode work)

…mean the agent can run tools and shell actions with little or no interactive approval. That is convenient for unattended mobile control and dangerous on a shared or sensitive host.

Pi has no sandbox/permission CLI flag and is already permissive for ordinary and delegated launches. Calendar may append `--no-extensions` so extension `ui.confirm` gates cannot stall unattended work.

Recommendations:

1. Prefer a **manual / safe** profile on machines with secrets or production access.
2. Use autonomous / bypass profiles only on disposable workspaces you accept losing.
3. Remember zen can read agent transcripts under the agent’s own home directories once a session exists.

## Credentials

### Direct Claude entry design

Zen follows the boundary documented by [yetone/magpie](https://github.com/yetone/magpie):
Magpie uses a localhost gateway and per-agent process/config projection, while
its Claude subscription path invokes the genuine local Claude binary. Zen's
equivalent path is implemented in `daemon/cmd/zen/claude_cli.go` (PATH shim,
environment scrub, native binary resolution), `daemon/cmd/zen/control_app.go`
(`claude_launch` route binding), and `daemon/modelprofiles/compile.go`
(route-scoped `ANTHROPIC_BASE_URL` and non-secret placeholders). The installer
writes only the credential-free `~/.local/bin/claude` shim; provider credentials
remain in Zen's private store and are injected by the daemon router at request
time.

For **Official login / Direct**, authenticate each CLI on the daemon host using
its own login flow. For a custom Codex or Claude endpoint, **Settings > Providers >
Models and accounts** stores the supplied API key on the current daemon through
Zen's credential store. The mobile form does not display stored secrets; leaving
an existing key empty preserves it. Switching the current server rebinds the
configuration. Selecting a Provider connection does not change the Brain
executor. Provider settings own the connection, endpoint, credentials, model
catalog, and exposed model scope; they do not own an Agent default model. A
Claude Code launch with no Agent-level model request omits `--model`, so Claude
Code inherits the local model saved through `/model` in its settings. An
Agent-level model selection is passed only for that Session. Codex follows the
same contract: an empty launch override leaves model selection to the local
Codex configuration and the first request model is routed unchanged.

Provider model availability prefers the live Provider catalog and its last-known-good
cache. When discovery is unavailable, Zen projects local Codex/Claude contract
IDs, installed caches, models.dev metadata and exact configured/session IDs as
display-only candidates with a freshness warning; those fallback rows do not
reject a custom Agent request. Zen may enrich discovered rows from the
secret-free public `models.dev` metadata cache (display name, limits, capabilities,
prices, and release date); that metadata never adds a routable model or re-enables
one disabled in Settings. The cache refreshes in the background and can be manually
refreshed through the daemon control path.
For a custom Claude gateway, enter its endpoint root (including any proxy path,
optionally ending in `/v1`). A model ID is an Agent/Session choice, not a
connection requirement. When a gateway has no `/models` endpoint, Zen keeps
last-known-good and local candidates for the Provider catalog; a Session may
still start and its request model is passed through. Failed model discovery
does not verify or invalidate the key. Zen routes managed Claude requests through its
session-bound loopback and injects the saved upstream key there. The launch uses
session-specific Claude settings to keep a pre-existing native settings file's
endpoint or authentication environment from redirecting that managed session;
native-login sessions retain their own authentication behavior. The same
session settings set `permissions.defaultMode` to `auto`, so a routed Claude
launch starts in Claude Code's Auto permission mode; an explicit
`--permission-mode` or `--dangerously-skip-permissions` on the launch command
still takes precedence, and Zen never rewrites the user's own
`~/.claude/settings.json`.
This loopback handoff applies to every Claude launch started through Zen. The
release installer places a small `claude` shim beside `zen`; from any new
terminal it asks the daemon for a route-scoped launch plan and then `exec`s the
native Claude binary. The shim removes inherited `ANTHROPIC_*` and Bedrock /
Vertex / Foundry overrides before applying only the process-local Zen values.
It never writes credentials to shell startup files. The native binary is
resolved from the remaining `PATH` entries, so the shim cannot recurse.

The direct entry requires the Zen daemon and a saved Claude Provider
connection/default. If no Zen Claude connection is selected it fails with an
actionable error instead of silently falling back to Anthropic login. To run
the unmodified native CLI, invoke its absolute path (or temporarily remove
the Zen install directory from `PATH`).

This shell takeover is only the interactive user entry point. Brain and other
control-plane callers use the executor catalog name `claude` through the
provider-neutral `zen worker spawn -executor claude` and `zen worker send`
protocol. They do not invoke `zen claude`, add a Claude-specific completion
path, or use a separate Worker lifecycle; Claude-specific startup and input
readiness remain inside its executor adapter.

## Custom executors

Any `[[executors]]` entry with `name` + `command` overrides or extends the map. Unknown tools are treated as custom tmux-backed agents.

## Delegated resource lifecycle

Visible Brain-delegated sessions and Calendar-launched Work run as plain processes in tmux. Zen does not create cgroups/scopes, apply memory or process limits, run a supervisor, enforce pool budgets, or refuse launches because of resource pressure.

Each delegated Session retains a daemon-namespaced ownership token and a short private temporary directory under `~/.zen/t/<digest>`. `TMPDIR`, `TMP`, `TEMP`, and `ZEN_BUILD_TMPDIR` point there. The inherited `ZEN_WORKER_RESOURCE_UNIT` identifies descendants even after detachment. Observed PID/start-time identities are persisted in ownership leases; cleanup follows these identities and child relationships, checks the boot identity and PID generation, sends TERM, then KILL only to remaining exact owned processes. User sessions and the Brain host are outside delegated cleanup. Closing a Worker or accepting completed Work still performs this cleanup. Observation alone never kills an orphan.

Live Sessions from the previous release are re-observed after daemon restart. Legacy scope-shaped tokens and lease metadata remain readable; the old processes/scopes keep running and end naturally. No startup migration stops them or creates new scopes. Only explicit ownership cleanup terminates their owned descendants.

Delegated working directories on volatile/memory-backed storage are still rejected to preserve durable work. Edit the supplied repository directly; use `ZEN_WORKTREE_ROOT` only for justified isolation. Ownership markers protect temporary-directory deletion, including against replaced symlink roots.

See [resource telemetry](resource-telemetry.md) for the machine snapshot, pressure thresholds and Brain events. `zen resources --json` reads the cached snapshot. Brain can ask a Worker to release tools, close it with `zen worker close`, or stop one selected owned tool tree with `zen worker release -id SESSION -pid PID -start START` using the exact identity in `consumers[].processes`. Release refuses the live provider and its ancestors and does not close the Session.

The Ghostty native build uses a disposable worktree because it must apply verified patches to an exact pinned source revision without mutating the shared checkout. Inside a delegated session that worktree lives under `ZEN_WORKTREE_ROOT` (default `~/.zen/worktrees`), not the owned temporary directory. Build temp may use the short owned `TMPDIR`/`ZEN_BUILD_TMPDIR`. A direct developer build uses `~/.cache/zen/build-tmp` on Linux or `~/Library/Caches/zen/build-tmp` on macOS; set `ZEN_BUILD_TMPDIR` to another durable absolute path when needed. This build-specific isolation does not imply one worktree per agent task.

## Structured chat update contract

Work/session Chat and Brain Chat use the same provider-neutral structured conversation subscription. The daemon publishes each logical event with a stable `id`; a provider-backed progressive update reuses that ID with new content and `partial: true`, and the canonical final update reuses it with `partial: false`. `transient: true` identifies a structured provider projection that may legitimately be absent from a later snapshot, so reconnect cleanup cannot duplicate an ephemeral reasoning/tool/status row. These optional fields preserve compatibility with older clients. `partial` describes lifecycle, not token granularity: it never authorizes the app to split or time-animate a completed body.

Update granularity is intentionally limited to what each current Zen adapter receives:

- **Genuine text delta — Grok:** `updates.jsonl` exposes native message/reasoning deltas and cumulative tool-output snapshots. Zen follows the native `promptId + streamStartMs + kind` identity and incrementally tails that file; `chat_history.jsonl` supplies canonical final records.
- **Block-level — Codex:** rollout JSONL exposes semantic `agent_reasoning` records plus tool/status lifecycle events. The current rollout adapter receives the final assistant answer as one completed `agent_message`, not assistant text deltas.
- **Block-level — Claude Code and Cursor Agent:** Claude project JSONL and Cursor `agent-transcripts` contain completed semantic message/thinking/tool/turn records. Zen publishes newly persisted blocks promptly but does not pretend that a complete block is token streaming.
- **Block-level — Pi:** Zen owns an absolute `--session` JSONL path per live Session and projects the active parent chain after Pi's late first-assistant flush.
- **Block-level — OpenCode:** Zen reads the local SQLite database read-only, binds one `ses_*` by exact CWD plus start window, and projects message/part rows.

The CLIs themselves have richer direct protocols that the current tmux/transcript integration does not own: Codex app-server emits item-keyed assistant/reasoning/output deltas, Claude supports `stream-json` with partial message events, and Cursor supports `stream-json --stream-partial-output`. Genuine delta support for those providers requires a daemon-owned structured executor runtime that owns process or app-server I/O, translates native lifecycle notifications into this reducer, persists reconnectable snapshots, and coordinates send/interrupt/terminal attachment. Adding flags to the existing interactive tmux command is not sufficient, and parsing raw NDJSON from terminal chrome would violate the protocol boundary.

On Linux, managed Claude sessions read project JSONL from the owning process's `CLAUDE_CONFIG_DIR` (or its `HOME/.claude`) when available. Other hosts retain the established default-home lookup; custom per-process config roots have not been verified there. On all hosts, an explicit `--resume <id>` binds only that ID, even when its transcript is older than the ordinary discovery window; if the file is missing, Zen does not select a different same-directory chat. Provider connection changes apply to future launches, but Zen does not claim that changing the gateway route updates an already-running interactive Claude process. Its active Provider/model picker is unavailable until a native process-control owner can acknowledge such changes; the existing session and transcript remain intact.

Zen does not synthesize timed typewriter output and does not derive structured Chat from terminal screenshots, prompt echoes, or pane chrome. The live Terminal path remains independent and unchanged.

## Diagnostics

`zen doctor` reports which configured executors are on `PATH` and best-effort auth hints. Guided `zen setup` can write `~/.zen/executors.toml` for you (Safe vs Autonomous, Host/Delegated). It never runs sudo, never logs into providers, and requires explicit confirmation for Autonomous. Restart `zen` after setup (or after editing executor definitions/commands) so the daemon reloads the static catalog. Once zen is running, change only the Delegated Executor with `zen brain set-delegated <id>` — no restart.

## Worker model and reasoning defaults

Executor IDs identify the agent client (`codex`, `claude`, `pi`, and so on).
Model and reasoning effort are separate model-profile settings; do not create
capability-suffixed executor IDs such as `codex-medium` or `codex-high`.

Keep the delegated executor set to `codex`, then choose the Codex model profile
and its reasoning effort in **Settings > Providers > Models and accounts**. A
profile selecting `gpt-6-sol` with `medium` effort applies to future delegated
Workers while preserving the existing `codex` executor identity. Existing
Sessions retain their current route and model until explicitly changed.
