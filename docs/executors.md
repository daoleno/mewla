# Agents and executors

Mewla runs agent CLIs that are already installed and signed in on your computer.
An **executor** is a named launch command, such as `claude` or
`cursor-agent --force --sandbox disabled`. You need only one.

## Built-in executors

With no configuration file, Mewla uses these defaults:

| Executor | Command | Approval prompts |
| --- | --- | --- |
| `claude` | `claude` | The CLI's own policy |
| `codex` | `codex` | The CLI's own policy |
| `agent` (Cursor) | `cursor-agent --force --sandbox disabled` | Bypassed |
| `grok` | `grok --no-alt-screen --permission-mode bypassPermissions` | Bypassed |
| `pi` | `pi` | Pi has no approval mode; it is permissive |
| `opencode` | `opencode` | The CLI's own policy |
| `dsh` | `dsh` | Opens the DSH web harness instead of a terminal agent |

Sessions you start from the app use these commands. Brain Workers and Calendar
scheduled actions add flags so they can run unattended; see
[Permission bypass risks](#permission-bypass-risks).

A Session's type follows the program that actually runs. A Cursor Session
started with a Claude model is still a Cursor Session.

## Configure executors

The configuration file `~/.mewla/executors.toml` is optional. To write one with
prompts, run:

```sh
mewla setup
# or without prompts:
mewla setup --non-interactive --host codex --profile safe
```

`--host` chooses the executor that runs Brain. `--profile` is `safe` or
`autonomous`; autonomous asks for confirmation (`--yes` without prompts).
`mewla setup` never installs packages, runs `sudo` or signs in to providers.

Or write the file yourself. Each `[[executors]]` entry overrides a built-in
executor or adds a new one; an unknown command becomes a custom terminal agent:

```toml
[[executors]]
name = "claude"
command = "claude --model claude-opus-5-5"

[[executors]]
name = "aider"
command = "aider"
```

Restart `mewla` after changing the file. The file is only a catalog of launch
commands; nothing in it chooses which executor a Worker uses.

## Permission bypass risks

Several commands let an agent run tools and shell commands with little or no
approval. That is what makes unattended work from a phone possible, and it is
dangerous on a machine with secrets or production access.

**Sessions you start yourself** use the executor command as configured. The
built-in `agent` (Cursor) and `grok` commands bypass approvals; `pi` is always
permissive.

**Brain Workers and Calendar scheduled actions** always run unattended,
whatever your configuration says. Mewla adds:

| Agent | Added for unattended work |
| --- | --- |
| Codex | `--dangerously-bypass-approvals-and-sandbox` |
| Claude Code | `--permission-mode bypassPermissions` (a weaker mode such as `acceptEdits` is rejected) |
| Cursor Agent | `--force --sandbox disabled --trust --approve-mcps` |
| Grok | `--permission-mode bypassPermissions --sandbox off` |
| OpenCode | `--auto` |
| Pi | Nothing; already permissive. Calendar may add `--no-extensions` |

Recommendations:

1. On a machine with secrets or production access, use the **safe profile**
   below for the Sessions you start, and do not hand that machine's work to
   Brain.
2. Use Brain and scheduled actions on machines and workspaces you are willing
   to let an agent change without asking.
3. Remember that Mewla reads agent transcripts from each agent's own home
   directory, and that model providers see what agents send them.

### Safe profile

Put this in `~/.mewla/executors.toml` and restart `mewla` so Sessions you start ask
for approval in their terminal:

```toml
[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "claude"
command = "claude"

[[executors]]
name = "agent"
command = "cursor-agent"
kind = "cursor"

[[executors]]
name = "grok"
command = "grok --no-alt-screen"

[[executors]]
name = "pi"
command = "pi"
kind = "pi"

[[executors]]
name = "opencode"
command = "opencode"
kind = "opencode"
```

Delete the entries for agents you do not use. `mewla setup --profile safe` writes
an equivalent file.

## Worker routing

Brain chooses the executor, model and reasoning level for every Worker. Before
each launch it reads `routing.md` in its workspace, a few lines of plain
Markdown such as "use Codex with high reasoning for refactors". Brain rewrites
a line when you state a preference, when a new model appears, or when a choice
turned out badly. Mewla itself never parses the file, and upgrades never
overwrite it.

A Worker launch looks like this:

```sh
mewla worker spawn -name "Fix flaky test" -executor claude \
  -model claude-opus-5-5 -reasoning high -cwd /path/to/repo -prompt "..."
```

Without `-executor`, the Worker uses Brain's own executor. To move Brain itself
to another agent, keeping its conversation:

```sh
mewla brain executors --json   # Brain's executor and the available ones
mewla brain use codex
```

### Model and reasoning flags

Mewla maps `-model` and `-reasoning` to each client's own flags:

| Executor | `-model` becomes | `-reasoning` becomes |
| --- | --- | --- |
| `codex` | `--model` | `-c model_reasoning_effort=...`: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra` |
| `claude` | `--model` | `--effort`: `low`, `medium`, `high`, `xhigh`, `max` |
| `pi` | `--model provider/id` | `--thinking`: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `grok` | `--model` | Not supported |
| `agent` (Cursor) | `--model` | Not supported |
| `opencode` | `--model provider/model` | Not supported |

A launch value replaces the same flag in the configured command. An executor
that cannot take the requested option fails the launch with an explanation
instead of silently dropping it. Empty values keep the client's default.
Overrides need a plain command, without shell syntax or a `--` separator.

Keep executor names for clients, not for capabilities: do not create
`codex-high` or `codex-medium`. Model and reasoning are chosen per launch.

Older configuration files may contain `delegated_executor`,
`delegated_model` or `delegated_reasoning`. These keys are ignored, and
`mewla doctor` points them out; you can delete them.

## Check your agents

```sh
mewla doctor
```

`mewla doctor` reports which configured agents are on `PATH` and gives hints when
one looks signed out. Model endpoints and API keys for Codex and Claude are set
in the app; see [Providers and usage](providers-and-usage.md).
