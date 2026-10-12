# Watch

Watch looks over your projects and tells you what needs you. It gathers
scattered signals into a small number of problems worth your attention.

Watch collects signals all the time, for free. When you turn Watch on, an
agent also reads new signals and turns them into a few problems, ranked by
what they cost you. The in-app view comes in a later release. Until then you
can inspect everything with `mewla watch`.

## Projects

You don't set projects up. A project is a git repository. Mewla finds it by
walking up from the working directory of each Session and each piece of Work
to the repository root. A worktree, including one Mewla creates for a Worker,
belongs to the repository it was made from.

Each project is named after its folder and records its git remotes, with any
credentials in the URL removed. To rename one:

```sh
mewla watch projects
mewla watch project rename --project <id> --name "Billing"
```

## Signals

Watch records four kinds of signal:

| Source | What it records | Project |
|---|---|---|
| Human | What you said in Brain and Worker conversations, with the reply you were answering. Every agent Mewla supports is read: Claude Code, Codex, Pi, Cursor, Grok, OpenCode and dsh. Prompts that Brain wrote for a Worker are kept separate from what you typed. | The Session's working directory |
| Process | Problems in the Work store: lost or stalled turns, automatic resumes, reconcile requests, ambiguous deliveries, input requests, failed turns, follow-up turns sent to a Worker, and Work reopened after it was closed. | The Work's working directory |
| Runtime | Problem lines from the daemon log ([Troubleshooting](troubleshooting.md#reading-the-daemons-terminal-output)) | Mewla |
| External | From connected [Plugins](plugins.md), read only. GitHub: failed CI runs on the default branch, open issues and pull requests with new activity, and Dependabot and code scanning alerts where your grant allows. Linear: issues and comments for the team you linked. | GitHub: by the repository's remote. Linear: by the team you confirm. |

Collection is local and doesn't run a model, so it is always on, whatever the
Watch setting says. Local sources are read about once a minute and the daemon
log every 30 seconds. GitHub and Linear are read every 7 minutes, and GitHub
reads pause when the remaining rate limit runs low. The first run reads the
last three weeks.

API keys, tokens and passwords are removed from every signal before it's
stored. Text fetched from GitHub or Linear is only stored as data. Watch never
acts on it.

### GitHub and Linear go through Plugins

Watch reads GitHub and Linear through the same [Plugins](plugins.md) Brain
uses, and follows the same switches:

- An account that is off or disconnected is not read.
- Watch uses only read tools, and only the ones allowed for that account.
  GitHub: `get_repository`, `list_workflow_runs` (CI), `list_updated_issues`,
  `list_dependabot_alerts` and `list_code_scanning_alerts`. Linear:
  `list_teams`, `list_issues` and `list_comments`.
- Watch's reads don't change an account's status or its recent calls.

`mewla watch projects` shows, for each repository and each linked Linear team,
whether Watch is collecting and, if not, why: `not_connected` (no account for
that service), `disabled` (every account is off), `not_allowed` (a read tool
it needs is blocked), or `unreachable` (no account can read it, for example
HTTP 404). An endpoint it can't use is listed under `github.endpoints`.

GitHub accounts connected before these read tools existed need **Refresh** in
Tools & activity and the new tools allowed (or **Read and search** turned off
and on again).

### Linking Linear

Watch suggests Linear teams for a project. A suggestion comes from a team
whose name matches the project's, or from Linear issue links in the project's
conversations. Confirm a suggestion once to start collecting that team. An issue filed under
a Linear project named like one of your projects goes to that project instead:

```sh
mewla watch projects --project <id>      # shows linear.suggested
mewla watch project link-linear --project <id> --team PER --confirm
```

## Problems

A few clear-cut problems are flagged immediately, without a model:

- **CI is red.** The latest CI run on the default branch failed. Watch also
  shows how long it has been failing.
- **Repeated daemon error.** The same daemon log error appeared at least five
  times within ten minutes in the last week.
- **Worker stalled or lost repeatedly.** One piece of Work lost or stalled its
  Worker at least twice in the last week.

### Judgment

With Watch on, new signals are handed in batches to one agent run with no
tools. It does three things:

- **Places** signals that have no project, by what they say: messages to
  Brain, conversations in folders that were renamed or deleted, and Work whose
  turn never reached a transcript. When the evidence is weak it leaves them
  unplaced.
- **Says who wrote** a message in a Worker conversation: you, a Brain
  follow-up, or a routine continuation.
- **Raises problems**: it separates problems from noise, merges signals from
  different sources about the same thing (rule flags included), estimates the
  cost (your attention, or the project's health), and marks problems that look
  fixed. Each problem keeps its evidence and a one-paragraph reason.

Before that, signals whose turn, Session or Work later turned up in a known
repository are placed without a model.

Judgment uses Claude Code (`claude -p`) with Sonnet, signed in the way your
own Claude Code is, without tools, hooks, MCP servers or a saved session. A run takes at most 300 signals or about
60,000 characters. A full batch runs at once; a partial one waits 30 minutes.
There are at most 60 runs a day, and a failed run waits 10 minutes and retries
with a smaller batch. Nothing runs when there are no new signals. Signal text
is treated as data, never as instructions, and secrets are removed again
before it is sent. Each run's tokens and cost are logged.

```sh
mewla watch enable
mewla watch status                 # judgment: pending, runs, tokens and cost
mewla watch judge --batches 3      # judge now instead of waiting
mewla watch problems --rule judgment
mewla watch signals --source human --kind user_message
```

### Telling Watch it was right

Label a problem `real`, `not` (not a problem) or `later`, with an optional
note. `real` confirms it and `not` dismisses it. Your labels and notes are
given to later runs, so they learn what you care about.

```sh
mewla watch problem label j_1a2b3c4d5e6f not --note "flaky runner, not our code"
```

## Storage

Everything lives in `~/.mewla/watch/`: `config.json` (the setting),
`projects.json`, `signals.jsonl` (append-only), `cursors.json` (how far each
source has been read), `problems.json`, `index.json` (which working directory
each turn, Session and piece of Work used), `annotations.json` (placements and
authors found after collection, and which signals judgment has seen) and
`judge-runs.jsonl` (each run's size, results, tokens and cost). Running a
collection pass again adds nothing that's already stored.
