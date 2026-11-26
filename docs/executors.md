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
| `agy` (Antigravity) | `agy` | None: Mewla adds `--dangerously-skip-permissions` |
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
only. So does the model beside **Send**, which shows while you write in the
Session's composer. Brain picks each Worker's agent and model itself; see
[Worker routing](brain-and-work.md#worker-routing).

### A Session's models

A Session lists what its agent offers with your sign-in, read from the agent
itself, and switches the agent's own way. A Claude Session on a
[Model Provider](#model-providers) does too: its route passes the model Claude
asks for through. A Codex Session on a Provider connection lists that
connection's models.

| Agent | The list | A switch |
| --- | --- | --- |
| Claude Code | Its model picker (the `initialize` answer of `claude -p`), with each model's effort levels, each named with the version Claude gives it ("Opus 5.5"; Default with the model it runs on now) | Made in Claude's own picker with its "use this session only" key: the conversation goes on, and your default for new sessions stays as it is. After a turn Claude asks whether to switch (the next answer re-reads the conversation); your pick is that answer. Whether the switch was made is read from Claude's own record of it (its answer to `/model` in the conversation file), so a Session switched many times reports each one rightly |
| Codex | `model/list` of the Session's own app-server, with each model's efforts | Applied to the thread, before its first message too (its first turn runs on it). Codex reads the thread's model as a turn starts, so a pick while it works is made when that turn ends; a message you send while it works joins the running turn, on the model it started with |
| OpenCode | The service's models of the providers you signed in to, with their variants as efforts | `session.switchModel`: the next turn uses it. Until its first message OpenCode's own screen holds the model, so the choice appears once that message is sent |
| Pi | The models Pi can use with your keys (your scoped ones when you scoped some), with the thinking levels Pi takes for each (no Default: Pi always thinks at one of them) | `pi.setModel` and `pi.setThinkingLevel` through Mewla's extension: the conversation goes on, your default stays |
| Cursor Agent, Grok | `cursor-agent models`, `grok models` | Their own `/model <id>`, typed into the Session |
| DSH, Antigravity | The agent's own models | Applied at once |

Claude, Codex, Cursor Agent and Grok take a switch between turns: a pick made while
the agent works, or waits for your answer, shows with an hourglass, the Model
sheet says "Opus 5.5 after this turn", and it is made when the turn ends. The
sheet, if still open, then shows the new model checked. A pick the agent did
not take then is said in the sheet with the agent's reason, and as a notice in
the Session. A switch between turns answers within seconds or fails with
Claude's own last line; Mewla types `/model` only into Claude's empty prompt,
never over your draft.

A Session offers no model choice while its agent cannot give one: not signed
in, a Codex still on a startup question or started by hand outside Mewla, a Pi
that has not linked up yet.

Listing an installed agent does not start its command or check its sign-in.
The menu says "Sign-in not checked" until Mewla has used that agent. A verified
signed-out agent says "Not signed in". `mewla doctor` runs explicit version and
sign-in checks for every configured agent when detailed diagnosis is needed.

## executors.toml

`~/.mewla/executors.toml` is optional. `mewla setup` writes one with prompts.
Each write (setup, `mewla brain executors add`) saves the file it replaces as
`executors.toml.bak.<UTC time>` and removes the older backups of that name, so
only the newest is kept.

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

`mewla doctor` shows which account each executor is signed in as; Brain's
menu › **Switch executor** shows it under a row only when two sign-ins of one
agent are installed, so they can be told apart. The
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
| Antigravity | `--dangerously-skip-permissions`, handed to `agy acp`. A command Mewla cannot run through `agy acp` (a flag beyond `--model`, `--effort`, `--mode`, `--conversation`, `--add-dir` and the permission switch) starts agy's own interface instead, with one inline `--settings` for the launch that trusts the Worker's folder and opens agy's tool and plan review gates. Nothing is written to your `~/.gemini`. A `--mode`, `--sandbox`, `-p` or `--continue` in the command is refused for delegated work |
| OpenCode | `--auto` |
| Pi | Nothing: it has no approval gate. Calendar may add `--no-extensions` |

Earlier versions started Claude Code in `--permission-mode auto`, where a
classifier could block work you had approved. That mode is gone: a command
written with `--permission-mode auto`, in `executors.toml` or by an older app,
now starts in `bypassPermissions`. Nothing in your files is rewritten; Mewla
changes the command as it loads and launches it.

Claude Code asks you to accept a one-time warning the first time it runs in
bypass mode. Mewla answers it for the Claude it launches, through a settings
file it passes with `--settings` (or, for a Claude that already gets Mewla's
inline `--settings` for its Provider route, in that same JSON, together with
the hooks Mewla follows it by); it does not edit your `~/.claude` files. A
Claude command that names its own `--settings` file is left alone, so put
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
Codex and Claude Code send model requests. **Agents** lists each of them with
what it uses now; tap one to choose its connection. **Providers** lists every
connection once; tap one for its page: its endpoint, its key, a connection
test, its models and Delete.

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
request. Running Claude Sessions use a new connection from their next
request. When Codex asks for a model, the selected Codex connection answers it
whenever it may serve it, even before its model list has been fetched.

**Any agent on any Provider.** A Custom Gateway may speak another API than
its agent: OpenAI Chat Completions (most local servers and model vendors),
Anthropic Messages, or OpenAI Responses. Mewla finds out which when you save
the connection, by sending each endpoint an empty request (no model is asked
anything), and then carries each request across. So Claude Code runs on a
Chat Completions endpoint, and Codex on a Chat Completions or an Anthropic
one. Streaming, tool calls (parallel ones too), images, reasoning effort and
token usage come through. Toward Anthropic, Codex keeps Claude's thinking
from one turn to the next, and the prompt is marked for caching, so a long
Session reads most of each prompt from cache. When Claude Code asks for its
own Haiku or Opus (for titles, summaries, Explore subagents) and the Provider
does not serve it, a model the Provider does serve answers instead. For any
model that is not Claude, the billing line Claude Code puts at the start of
its prompt is dropped: it changes every turn and would keep the Provider from
reusing its cache. Codex on a translated endpoint uses HTTP, not
WebSocket. Moving an agent back to a Provider that speaks its own API keeps the
conversation going: what only the translation wrote into its history (Claude's
sealed thinking for Codex, a Chat model's reasoning for Claude Code) is left
out of what that Provider is sent.

**When a Provider fails.** A Provider that is busy (a 5xx or 529 status, or a
dropped connection) is asked twice more, after 1 and 2 seconds. A rate limit is
asked three more times, after 1, 2 and 4 seconds. Either way, Mewla waits what
the Provider's `Retry-After` says instead, if that is 8 seconds or less. This
holds for Sessions Mewla starts and for CLIs in a plain terminal. Nothing is
sent again once the reply has started, and never for image generation or web
search, which are billed per call. A longer wait, a used-up quota or balance,
and every other error go straight to the agent. When Mewla itself cannot
forward a request, the agent shows why and where to fix it, for example `Mewla
has no API key for Provider "xcode". Add it in Mewla: Settings › Model
Providers.` or `Mewla could not reach Provider "Home GPU": the connection was
refused`. Problems that asking again cannot fix (no key, no Provider chosen)
are answered so that neither agent spends minutes retrying them. A Provider on
another of your computers over Tailscale is reached like one on your LAN.

**A fallback for a Provider.** A connection can name another connection of
the same agent to take its requests while it fails:
`mewla providers fallback set <fallback-id> --connection <id>` (and `clear`;
`mewla providers fallback` lists them). When the connection cannot be reached,
stays busy after the retries above, is rate-limited, is out of credit or quota,
or refuses its key, the request goes to the fallback before the agent sees an
error, and the connection rests: 30 seconds at first, longer each time it
fails again (up to 10 minutes; a rate limit rests until the time the Provider
names, a used-up balance 15 minutes). Its requests go to the fallback while it
rests and come back to it after. A Claude model the fallback does not serve is
answered by one it does, and Codex's model goes as the fallback's own. Two
connections that both speak the agent's own API (two Anthropic endpoints for
Claude Code, two Responses endpoints for Codex) are not paired: each would
refuse the thinking the other signed in the conversation.

**Other tools that switch your CLIs.** Mewla shares these files with you and
with tools such as Magpie that point Codex or Claude Code at a gateway of their
own. It changes a value only while the value is still the one it wrote: a
`model_provider` or `ANTHROPIC_BASE_URL` someone set since is left alone, and
a daemon restart never takes a CLI back from another tool (choosing a Provider
in Mewla does). Choosing Official login gives back what you had before, and so
does `mewla boot uninstall` (on Mewla for Mac, **Stop Mewla on this Mac…**); a
daemon that stops or restarts by itself, or for an update, keeps both CLIs on
the gateway, which comes back on the same address. If
another tool later writes Mewla's gateway back while Mewla is not using it
(it restores what it saw while it was on), Mewla notices within seconds and
gives the CLI your own provider again, so Codex never names a gateway that is
gone. Codex resumes a chat only on the provider its record names ("This chat
uses model provider ... which is no longer configured"), so Mewla moves that
record along with the provider each way. Codex's chat list can still group a
chat under the provider it started on. A model the
Provider added since Mewla last listed its models is looked up once before
the gateway refuses it. A second daemon run for a test beside your own
(`MEWLA_NO_CONFIG_TAKEOVER=1`) leaves both files alone; with a `CODEX_HOME` of
its own it points that copy's Codex config at its gateway instead.

**Prompt cache.** Claude Code on an API key or a gateway keeps its prompt
cache for 5 minutes, so a Session that waits longer (a build, a test, a reply)
pays for its whole context again at the next request. A Claude Session Mewla
starts on an API key, a token or an `apiKeyHelper` gets
`CLAUDE_CODE_PROMPT_CACHE_TTL=1h` in its environment, which keeps it for an
hour (1-hour cache writes cost more than 5-minute ones). A subscription sign-in
is left alone (Claude Code already keeps an hour there, within its usage
limits).

To keep 5 minutes, or to pick the lifetime yourself, set it the way you would
without Mewla, and Mewla never overrides it: `"promptCacheTtl": "5m"` (or
`"1h"`) in a Claude Code settings file, or `CLAUDE_CODE_PROMPT_CACHE_TTL` in the
executor command, the daemon's environment or a settings `env` block. Mewla
reads the settings files Claude Code reads for that Session: your
`settings.json` (in `CLAUDE_CONFIG_DIR` if set), the project's
`.claude/settings.json` and `.claude/settings.local.json`, the `--settings` of
the command, and the managed-settings file of this machine
(`/etc/claude-code`, `/Library/Application Support/ClaudeCode` or
`C:\Program Files\ClaudeCode`, with its `managed-settings.d` folder). So does
`FORCE_PROMPT_CACHING_5M`. Managed settings that reach Claude Code another way
(the claude.ai console, a macOS profile, the Windows registry) are not read:
set the variable in those Sessions' executor command instead. Mewla does not
write to your settings files for this.

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

**Models.** A Provider's page turns each of its models on or off for every
agent using that Provider, and opening it asks the Provider for its list again. A model that is off
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
each tool. Antigravity's own Skills are the ones in `~/.gemini/config/skills`
(where agy keeps them) and a project's `.agents/skills`; Mewla changes that
folder only when you delete a Skill there.

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

## Antigravity

`agy` is Google's Antigravity CLI (it took over from Gemini CLI for individual
accounts on 18 June 2026). Install it with
`curl -fsSL https://antigravity.google/cli/install.sh | bash` (macOS and Linux;
Windows: `irm https://antigravity.google/cli/install.ps1 | iex`), then run `agy`
once and sign in with Google OAuth in the browser; Mewla never types a
credential. Gemini 4 Neon and the other models of your plan appear in
`agy models`. `mewla doctor` runs `agy models` to see whether it is signed in.

Antigravity runs Workers from Brain, Calendar and the app, and can be Brain's own
agent (`mewla brain use agy`, or the Brain menu's Switch executor); the
conversation moves with Brain when it switches, as with any other agent.

Mewla runs every agy it starts (a New session, a Worker, Brain's own agent)
through agy's Agent Client Protocol server, `agy acp`, the public, versioned
protocol agy offers to editors. The Session's program is `mewla agy-session`:
it holds `agy acp` in the Session's terminal, sends it the messages, keeps the
conversation in a log of its own
(`<state>/provider-sessions/antigravity/<conversation>.jsonl`), and reports
every turn's start, end and token use, agy's tool calls, approvals and
questions to the daemon over a local socket. It is in the Session's terminal,
not in the daemon, so a daemon restart does not touch it: agy goes on with its
turn, and the program connects again and says what is still true (the
conversation, a turn still running, a decision still waiting). If agy itself
exits, the program starts it again on the same conversation (up to three times
in two minutes) and applies the Session's model, thinking level and mode
again, which agy does not keep with a conversation.

What this gives agy is what Claude Code and Codex have: Chat follows the
conversation as it happens; a message is confirmed delivered when agy takes
it; Stop cancels the turn; agy's approvals and its questions (its
`ask_question` tool) are answered in the app, or by Brain first for a Worker
it delegated; the Model sheet lists agy's model families and thinking levels
and a change applies at once; Stats counts each turn's tokens (agy runs on
your Google plan, so no price is shown); Mewla's browser and computer tools
reach agy as MCP servers given with the session. When agy carries on by itself
after a turn (a command it left running in the background finishes and it
answers), that is a turn of its own.

One thing differs from Claude Code: the Terminal shows Mewla's view of the
conversation, with a line to type on (Enter sends, Ctrl-C stops a turn, a
digit answers a waiting question), not agy's own full-screen interface. agy
offers no way to follow its full-screen interface turn by turn, and its hooks
are read only from `~/.gemini/config/hooks.json` or a repository's
`.agents/hooks.json` (the inline `--settings` hooks are deprecated), and
Mewla writes nothing into your `~/.gemini`. The conversation is agy's own:
`agy --conversation <id>` opens it in agy's interface at any time. An agy you
start yourself in a terminal and Mewla adopts is followed as before: its Chat
is replayed by `agy acp` (`session/load`) when its conversation file changes,
and messages are typed into it. Mewla does not parse agy's
`conversations/<id>.db`: those are undocumented protobuf blobs and agy updates
itself daily. agy updates itself in the background and has no switch for it.
