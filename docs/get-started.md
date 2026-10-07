# Get started

Mewla is for one person running many coding agents. A small Go daemon runs on
your Linux or macOS computer, next to your repositories, `tmux` and agent CLIs.
The Android or iOS app connects to that daemon. Your code, credentials and agent
state stay on your computer.

Mewla is in **beta**.

## The pieces

| Piece | Where it runs | What it does |
| --- | --- | --- |
| Daemon (`mewla`) | Your computer | Owns Sessions, Brain, Work, pairing and settings. One binary, no cloud service. |
| App | Your phone | Shows the daemon's Sessions, Brain and settings. It connects to exactly one daemon at a time. |
| Agent CLIs | Your computer | Claude Code, Codex, Cursor Agent, Grok, Pi, OpenCode. Mewla launches and reads them; you sign in to each one yourself. |
| `tmux` | Your computer | Every Session is a `tmux` session, so it is also there at your desk. |

## Sessions

Start an agent or a plain shell in any directory on your computer. On the phone,
open a Session as:

- **Chat**: the structured conversation, with messages, tool calls, plans and
  attachments. Available for Claude, Codex, Cursor, Grok, Pi and OpenCode.
- **Terminal**: the live terminal, rendered natively. Works for any command.
- **Git**: read-only All, Working and Staged diffs for the Session's
  repository. Reviewing never stages or discards anything.

## Brain

Give Brain a goal once. It records each part as durable **Work**, starts a
**Worker** (an ordinary Session) for it, reads the result and decides what
happens next. You can open any Worker and take over. Only Brain or you can mark
Work done. See [Brain and Work](brain-and-work.md).

## Around the agents

Everything below reads from the one daemon the app is connected to.

- **Model Providers**: your own OpenAI, Anthropic, DeepSeek, OpenRouter or
  custom gateway keys for Codex and Claude. See [Providers and usage](providers-and-usage.md).
- **Skills**: the Skills and Agent Plugins your agents load, and where each copy came from.
- **Plugins** (preview): Linear, Notion, GitHub, Slack, Google Workspace and
  custom services for Brain. See [Plugins](plugins.md).
- **Stats**: tokens, sessions and cost per model.
- **Calendar**: reminders and scheduled actions that run as Work. See [Calendar](calendar.md).
- **Services**: ports your Sessions opened, with an optional temporary public URL. See [Services](services.md).
- **Resources**: CPU, memory, disk and pressure, with processes attributed to Workers, Brain or you.
- **Alerts and Telegram**: a push only when a Session needs you, fails or
  finishes, and Telegram as a second channel. See [Notifications and Telegram](notifications.md).

## One owner, one server

Mewla serves one person. The daemon has its own key; each phone enrolls once with
a short-lived pairing code and then signs every request. The app follows
exactly one current server, and switching servers never mixes their data. See
[Security and privacy](security-and-privacy.md).

## What you need

- Linux (`amd64` or `arm64`), WSL2, or an Apple Silicon Mac
- `tmux`
- At least one agent CLI on `PATH`, already signed in
- An arm64 Android phone, or an iPhone with a source build or the TestFlight preview

## Your first ten minutes

1. [Install](install-daemon.md) the daemon and run `mewla doctor`.
2. Start it with `mewla --lan` and scan the pairing QR code with the app.
   [Connect and pair](connect-and-pair.md) covers other networks.
3. Open **Sessions**, start an agent in one of your repositories and switch
   between Chat and Terminal.
4. Open **Brain** and give it a small goal.
5. Before you leave an agent running unattended, read
   [Permission bypass risks](executors.md#permission-bypass-risks).

## Status

| Area | Status |
| --- | --- |
| Daemon on Linux, WSL2 and Apple Silicon macOS | Beta, released |
| Android app (arm64 APK) | Beta, released |
| iOS app | Source build; a TestFlight preview is awaiting Apple review |
| Brain, Workers and Work | Beta |
| Calendar scheduled actions, Telegram | Beta |
| Plugins | Preview; first-time sign-in is not yet available for every service |
| Web UI in a browser | Available from the daemon; not the main client |

Known issues are listed under [Releases](releases/README.md#known-issues).
