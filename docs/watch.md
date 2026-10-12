# Watch

Watch looks over your projects and tells you what needs you. It gathers
scattered signals into a small number of problems worth your attention.

This page describes what Watch collects today. Grouping signals into problems
with an agent, and the in-app view, come in later releases. Until then you can
inspect everything with `mewla watch`.

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

Watch collects locally and doesn't run a model. Collection is always on,
whatever the Watch setting says. The setting turns on the analysis that later
releases add. Local sources are read about once a minute and the daemon log
every 30 seconds. GitHub and Linear are read every 7 minutes, and GitHub reads
pause when the remaining rate limit runs low. The first run reads the last
three weeks.

API keys, tokens and passwords are removed from every signal before it's
stored. Text fetched from GitHub or Linear is only stored as data. Watch never
acts on it.

Watch reads GitHub and Linear on the daemon's own behalf. It only makes
read-only requests to a fixed list of endpoints. This doesn't depend on the
per-tool permissions you give Brain in Plugins, and it never changes an
account's status or call history.

### Linking Linear

Watch suggests Linear teams for a project. A suggestion comes from a team
whose name matches the project's, or from Linear issue links in the project's
conversations. Confirm a suggestion once to start collecting that team:

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

```sh
mewla watch problems
mewla watch signals --source external --kind ci_failed
```

## Storage

Everything lives in `~/.mewla/watch/`: `config.json` (the setting),
`projects.json`, `signals.jsonl` (append-only), `cursors.json` (how far each
source has been read), `problems.json` and `index.json` (which working
directory each turn, Session and piece of Work used). Running a collection
pass again adds nothing that's already stored.
