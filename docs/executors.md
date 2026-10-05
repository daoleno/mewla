# Executors

Zen launches and observes AI CLIs through tmux. Configuration is optional: if `~/.zen/executors.toml` is missing, built-in defaults apply.

Session identity follows the live provider executable. A Cursor Worker launched
with `cursor-agent --model claude-opus-5-5-high` is a Cursor Session; a model
argument containing `claude` does not select the Claude transcript reader.
Visible delegated Workers are included in the mobile Session index even when
their lifecycle status is `unknown`. That status means Zen lacks authoritative
turn evidence, and does not mean the provider process stopped. Sessions use the
current server's index on both Android and iOS.

**One authenticated executor on `PATH` is enough** for a usable Zen installation. You do not need Codex, Claude, Cursor, Grok, Pi, and OpenCode all installed.

## Built-in defaults (current code)

When the file is absent, zen uses approximately:

| Name | Default command | Notes |
| --- | --- | --- |
| `codex` | `codex` | |
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

Executor definitions are a static catalog of launch commands. Nothing in the
file selects which executor a Worker uses.

## Worker routing

Brain picks every Worker's executor, model and reasoning for the task at hand.
It reads `routing.md` in its workspace before each spawn and always passes all
three:

```sh
zen worker spawn -name "Fix flaky test" -executor claude -model claude-opus-5-5 -reasoning high -cwd /repo -prompt "..."
```

`routing.md` is plain Markdown judgment guidance, not configuration. Zen never
parses it. The first workspace creation seeds a short generic guide, and product
upgrades never overwrite it. After that, Brain and the user maintain it. Brain
revises it when the user states a routing preference, when a new executor or
model becomes available, or when a Worker result shows a routing choice was a
poor fit. It replaces outdated lines instead of appending, so the guide stays
short.

Per-launch model and reasoning map to each client's own flags (verified against
codex 0.159.3, claude 2.1.289 and pi 0.99.1):

| Executor | `-model` | `-reasoning` |
| --- | --- | --- |
| `codex` | `--model` | `-c model_reasoning_effort="…"` (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra`) |
| `claude` | `--model` | `--effort` (`low`, `medium`, `high`, `xhigh`, `max`) |
| `pi` | `--model provider/id` | `--thinking` (`off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`) |
| `grok` | `--model` | unsupported |
| `agent` (Cursor) | `--model` | unsupported |
| `opencode` | `--model provider/model` | unsupported |

A launch value replaces the same flag in the catalog command (for example a
catalog `claude --model X` becomes `claude --model Y --effort high`). A client
or level that cannot take the requested option fails the spawn with an
explanation; nothing is silently dropped. Empty values keep the client's own
default. Model/reasoning overrides need a plain command without shell
composition or a `--` terminator.

When no one chooses an executor (`zen worker spawn` without `-executor`, or a
Calendar `scheduled_action`), the Worker runs on the Brain host executor. That
executor is the one the user picked for Brain and is known to work. It still has
no model or reasoning override.

Older `~/.zen/executors.toml` files may still contain `delegated_executor`,
`delegated_model` or `delegated_reasoning`. They load normally, but these keys
are ignored, and `zen doctor` prints a one-line note that they are retired.
Delete them at any time. The `zen worker defaults` and `zen brain set-delegated`
commands and the `ZEN_DELEGATED_EXECUTOR` lock no longer exist.

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

### Machine-level gateway

Zen runs one local gateway (`127.0.0.1:3425`, or the next free port) for every
client. It is a passthrough: each request goes to the currently selected
Provider connection for that client, with the placeholder credential replaced by
the connection's stored key. The body is forwarded unchanged, except that a
Claude `[1m]` model suffix is removed because no Provider serves it. Claude
requests go to the selected Claude connection whatever model they name; Codex
requests are resolved by model, preferring the selected Codex connection.

On every start, the daemon projects the gateway into each client's own
configuration, so a process started outside Zen (a plain shell, an IDE) is
routed without a wrapper:

- Codex: a marked block in `CODEX_HOME/config.toml`, with an exact backup of
  the previous file.
- Claude Code: `env.ANTHROPIC_BASE_URL` (the gateway root, no `/v1`) and, unless
  the connection uses native login, a non-secret `env.ANTHROPIC_AUTH_TOKEN`
  placeholder in `~/.claude/settings.json` (`CLAUDE_CONFIG_DIR`-aware). Every
  other key is preserved and symlinks are followed. The original bytes are
  backed up under the daemon state directory, and the user's previous values
  for these two keys are restored if no Claude connection is selected. A value
  the user edits afterwards is never overwritten on restore.

Selecting a Provider connection is the only control: there is no enable step
and no separate client command. While the daemon is stopped, the projected
endpoint refuses connections; restore the backup or edit the two `env` keys to
use a client without Zen.

For **Official login / Direct**, authenticate each CLI on the daemon host using
its own login flow. For a custom Codex or Claude endpoint, **Settings > Agents >
Model Providers** stores the supplied API key on the current daemon through
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
still takes precedence. The session settings outrank the machine-level
gateway projection in `~/.claude/settings.json`, so a managed session keeps its
own route.
Brain and other control-plane callers use the executor catalog name `claude`
through the provider-neutral `zen worker spawn -executor claude` and
`zen worker send` protocol; Claude-specific startup and input readiness remain
inside its executor adapter.

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

On Linux, managed Claude sessions read project JSONL from the owning process's `CLAUDE_CONFIG_DIR` (or its `HOME/.claude`) when available. Other hosts retain the established default-home lookup; custom per-process config roots have not been verified there. On all hosts, an explicit `--resume <id>` binds only that ID, even when its transcript is older than the ordinary discovery window; if the file is missing, Zen does not select a different same-directory chat. Selecting a Claude Provider connection updates future launches and retargets every routed, running Claude session in the same transaction: the process keeps its loopback route URL and client model, and its next request reaches the newly selected connection. Cross-provider history is sent through the portable strip boundary (thinking blocks removed). The per-session Provider/model picker stays unavailable because no native process-control owner can acknowledge a model change.

Zen does not synthesize timed typewriter output and does not derive structured Chat from terminal screenshots, prompt echoes, or pane chrome. The live Terminal path remains independent and unchanged.

## Diagnostics

`zen doctor` reports which configured executors are on `PATH` and best-effort auth hints. Guided `zen setup` can write `~/.zen/executors.toml` for you (Safe vs Autonomous, Brain host). It never runs sudo, never logs into providers, and requires explicit confirmation for Autonomous. Restart `zen` after setup (or after editing executor definitions/commands) so the daemon reloads the static catalog.

## Executor identity and model choice

Executor IDs identify the agent client (`codex`, `claude`, `pi`, and so on).
Model and reasoning effort are separate. Do not create capability-suffixed
executor IDs such as `codex-medium` or `codex-high`. Brain passes `-model` and
`-reasoning` per Worker (see [Worker routing](#worker-routing)), and Model
Provider profiles in **Settings > Agents > Model Providers** still apply their
connections at launch. Existing Sessions retain their current route and model
until explicitly changed.
