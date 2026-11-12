# Agents and models

Mewla runs the agent CLIs already installed and signed in on your computer.
This page covers which ones it runs, how much they may do without asking,
which model endpoints they use, and which Skills they load.

## Built-in agents

An **executor** is a named launch command. With no configuration, Mewla knows
these; one installed, signed-in CLI is enough:

| Executor | Command | Approval prompts |
| --- | --- | --- |
| `claude` | `claude` | None: Mewla adds `--permission-mode bypassPermissions` |
| `codex` | `codex` | None: Mewla adds `--dangerously-bypass-approvals-and-sandbox` |
| `agent` (Cursor) | `cursor-agent --force --sandbox disabled` | Bypassed |
| `grok` | `grok --no-alt-screen --permission-mode bypassPermissions` | Bypassed |
| `pi` | `pi` | None; Pi is always permissive |
| `opencode` | `opencode` (OpenCode 2 only) | None: Mewla adds `--auto` |
| `dsh` | `dsh` | Opens the DSH web harness, not a terminal agent |

A Session's type follows the program that actually runs: a Cursor Session using
a Claude model is still a Cursor Session.

## Choose Brain's agent and model

In the app, Brain's menu (**⋯** at the right of Brain's header) has:

- **Switch executor**: the installed agents with their provider, runtime and
  account, the current one checked, then "Install more with mewla deps". Pick
  one and Brain moves to it, with its thread. `mewla brain use <executor>` does
  the same from the computer.
- **Model**: the current agent's models, then **Effort** (**Low**, **Medium**,
  **High**).

A Session's own **⋯ › Model & Effort** opens the same sheet for that Session
only. Brain picks each Worker's agent and model itself; see
[Worker routing](brain-and-work.md#worker-routing).

## executors.toml

`~/.mewla/executors.toml` is optional. `mewla setup` writes one with prompts:

```sh
mewla setup
mewla setup --non-interactive --host codex
```

`--host` is the executor that runs Brain. Setup never installs packages, runs
`sudo` or signs in to providers. It has no permission question: every agent runs
with full authority (see below).

To edit it by hand, add one `[[executors]]` entry per command. An entry with a
built-in name overrides it; any other name adds a custom terminal agent:

```toml
[[executors]]
name = "claude"
command = "claude --model claude-opus-5-5"

[[executors]]
name = "aider"
command = "aider"
```

Restart `mewla` after a change. The file only lists launch commands; Brain
chooses which one each Worker uses (see
[Worker routing](brain-and-work.md#worker-routing)).

## More than one account

A second Claude Code or Codex sign-in, such as a work and a personal account,
is its own executor, picked in Brain's routing like any other:

```sh
mewla brain executors add claude-work -like claude
mewla brain executors login claude-work    # signs in to the second account
```

`add` writes an `[[executors]]` entry with a `config_dir` of its own
(`CLAUDE_CONFIG_DIR` for Claude Code, `CODEX_HOME` for Codex; by default under
`~/.mewla/accounts/<name>`, or pass `-config-dir`) and backs up the old
`executors.toml`. Restart `mewla` afterwards. The default sign-in is untouched.

```toml
[[executors]]
name = "claude-work"
command = "claude"
config_dir = "/path/to/claude_work"
```

`mewla doctor` and Brain's menu › **Switch executor** show which account each
executor is signed in as, so two sign-ins of one agent can be told apart. The
account is the email the CLI recorded at sign-in; it is blank for an API key.
A separate sign-in keeps its own conversations and settings, so the Model
Provider you picked and the Skills listed under **Extensions** apply to the
default sign-in only.

## Permission bypass risks

**Every agent Mewla launches runs with full authority.** There is no safe mode
and no setting that turns this off: agents run tools and shell commands without
asking, and nothing reviews an action before it runs. On a machine with secrets
or production access, that is dangerous.

This covers the Brain host, Brain Workers and Calendar's **Mewla actions**, and the
app's agent presets carry the same flags. Mewla adds these flags whatever the
configuration says:

| Agent | Added to every launch |
| --- | --- |
| Codex | `--dangerously-bypass-approvals-and-sandbox` |
| Claude Code | `--permission-mode bypassPermissions`: no per-action approval, and nothing reviews what it runs. An explicit `bypassPermissions` or `--dangerously-skip-permissions` in your command is kept; `default`, `acceptEdits`, `dontAsk` and `plan` are rejected for delegated work |
| Cursor Agent | `--force --sandbox disabled --trust --approve-mcps` |
| Grok | `--permission-mode bypassPermissions --sandbox off` |
| OpenCode | `--auto` |
| Pi | Nothing: it has no approval gate. Calendar may add `--no-extensions` |

Earlier versions started Claude Code in `--permission-mode auto`, where a
classifier could block work you had approved. That mode is gone: a command
written with `--permission-mode auto`, in `executors.toml` or by an older app,
now starts in `bypassPermissions`. Nothing in your files is rewritten; Mewla
changes the command as it loads and launches it.

Claude Code asks you to accept a one-time warning the first time it runs in
bypass mode. Mewla answers it for the Claude it launches, through a settings
file it passes with `--settings`; it does not edit your `~/.claude` files. A
Claude command that carries its own `--settings` is left alone, so put
`"skipDangerousModePermissionPrompt": true` in that settings yourself. Claude
refuses bypass mode for root outside a container, one more reason not to run
the daemon as root.

What stays: Mewla's own boundaries (pairing, device signatures, file and
upload limits), Brain not treating a message relayed from another computer as
your consent, and prompts the agent still raises on its own, such as an `ask`
rule in your Claude settings or an organization policy. A delegated Worker's
prompt goes to Brain first, then to your phone, and is refused with a reason if
nobody answers. Nothing else checks an action before it runs.

So:

1. On a machine with secrets or production access, do not run Mewla, or do not
   give that machine's work to Brain.
2. Use Brain and Mewla actions only where you accept an agent changing
   things without asking.
3. Model providers see what agents send them.

## Model Providers

**Settings › Model Providers** (tap the cat, then **Settings**) decides where
Codex and Claude Code send model requests. Pick the client, then its
connection:

| Connection | Requests go to |
| --- | --- |
| Official login | The provider, using the CLI's own sign-in on the computer |
| OpenAI, Anthropic, DeepSeek, OpenRouter | That provider, with your API key |
| Custom Gateway | Your endpoint, with your key |

Keys are stored on the daemon and never shown again; leaving the key field
empty keeps the saved one. For a custom Claude gateway, enter the endpoint
root, including any proxy path, optionally ending in `/v1`.

**How requests are routed.** The daemon runs one local gateway
(`127.0.0.1:3425`, or the next free port). On each start it points Codex (a
marked block in Codex's `config.toml`, backed up first) and Claude Code
(`ANTHROPIC_BASE_URL` and a placeholder token in `~/.claude/settings.json`) at
that gateway. So a CLI you start in a plain terminal or an IDE uses the
selected connection too. The gateway swaps in the real key and forwards the
request unchanged. Running Claude Sessions use a new connection from their next
request.

**Images and web search in Codex.** Codex's built-in `image_gen` and `web.run`
tools call their own endpoints (`/v1/images/generations`, `/v1/images/edits`,
`/v1/alpha/search`). The gateway sends these to the selected Codex connection,
once and without retry, so that connection must serve them (for example
`gpt-image-2`). Codex also hides `image_gen` while it is signed in to a free
ChatGPT plan, even when requests go through the gateway. To offer it anyway, add
`-c cli_auth_credentials_store="ephemeral"` to the Codex executor command. That
launch then ignores the stored sign-in and leaves the file in place.

Codex always asks `image_gen` for `gpt-image-2`. To use another image model, set
`image_model` on the Codex connection:
`mewla providers image-model set gpt-image-2.5-sunburst` (selected connection;
`--connection <id>` for another), `mewla providers image-model show`, and
`mewla providers image-model clear`. The gateway then replaces the model on
`/v1/images/generations` and `/v1/images/edits` (JSON or multipart bodies) and
leaves `/v1/alpha/search` alone. The next image call uses the new setting, with
no Codex restart. Without it, image requests pass through unchanged. The
setting is stored as `image_model` in `model-profiles.toml`.

While the daemon is stopped, the gateway refuses connections. To use a CLI
without Mewla, restore its backup or remove those two Claude settings.

**Models.** Mewla lists what a connection offers, from the provider's live
catalog when it can. Choosing a connection sets no default model: each CLI
keeps its own (for Claude Code, whatever you chose with `/model`). A model
picked for one Session applies to that Session only.

**Model access.** A Provider's **Model access** page turns each model on or
off for every agent using that Provider. A model that is off
is refused by the gateway, so a Session asking for it fails until it is turned
back on. Turning a model off asks for confirmation first.

When a provider cannot list its models, the Codex list comes from the installed
Codex itself (its app-server answers `model/list`, so new Codex models appear
without a Mewla update). If Codex does not answer, Mewla uses Codex's
`models_cache.json`, and last a short list built into the daemon. Each model in
the list shows which of these it came from (`codex_app_server`, `codex_cache` or
`bundled`); the daemon logs when Codex's app-server did not answer. Context
windows, names and prices of other models come from the public models.dev
catalog.

The Model Provider and Brain's executor are separate choices; changing one
never changes the other.

## Your agents' plugins and Skills

**Extensions** (tap the cat, then **Extensions**) leads with the services
Mewla itself connects; see [Extensions](extensions.md). Below them, **From your
agents** has one row per tool with what that tool
installed itself, such as "Codex · 6 plugins · 34 Skills". Tools with nothing
installed are named in one line underneath.

A tool's page lists its **Plugins** (Claude Code and Codex) and its **Skills**:
Codex built-ins and the tool's global and project Skills. Skills that come
with a plugin are listed inside that plugin. Skills in `~/.agents/skills` serve
several tools, so they are listed once, under **Shared**, rather than under
each tool.

Open a Skill to see its description, files, the agents it is available to and
every copy. **Delete Skill** removes exactly the copy you opened, after you
confirm. Built-in copies and copies owned by a plugin are protected. A plugin
shows the Skills, MCP servers and apps it brings, and can be uninstalled from
its page, after you confirm.

## Check your agents

```sh
mewla doctor
```

It reports which configured agents are on `PATH`, hints when one looks signed
out, and exits nonzero when the machine is not ready.
