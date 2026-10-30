# Watch

Watch looks over your projects and tells you what needs you. It gathers
scattered signals into a small number of problems worth your attention.

Watch collects signals all the time, for free. When you turn Watch on, an
agent also reads new signals and turns them into a few problems, ranked by
what they cost you. Watch has no screen of its own: problems reach you through
Brain, in the conversation you already have ([Reaching you](#reaching-you)).
You can inspect everything with `mewla watch`.

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

Watch records five kinds of signal:

| Source | What it records | Project |
|---|---|---|
| Human | What you said in Brain and Worker conversations, with the reply you were answering. Every agent Mewla supports is read: Claude Code, Codex, Pi, Cursor, Grok, OpenCode and dsh. Prompts that Brain wrote for a Worker are kept separate from what you typed. | The Session's working directory |
| Process | Problems in the Work store: lost or stalled turns, automatic resumes, reconcile requests, ambiguous deliveries, input requests, failed turns, follow-up turns sent to a Worker, and Work reopened after it was closed. | The Work's working directory |
| Runtime | Problems the daemon reports about itself (a failed plugin connection, a dropped upload, a collection that keeps failing), each with a kind, a severity and a subject. The daemon log carries the same problems as plain lines for you to read ([Troubleshooting](troubleshooting.md#reading-the-daemons-terminal-output)); Watch does not read the log. | Mewla |
| Notification | Every push Mewla sends to your phone, and every one it held back: the text as sent, the kind (a Worker needing input, failing or finishing; a Brain message; a scheduled result; a Watch alert), the Work or thread it was about, whether it was delivered, whether the app was in the foreground and whether you had that Session open. After the send it keeps what you did with it: tapped it, opened that Session, or brought the app forward. Push tokens are never stored. | The Session's working directory |
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
conversations. Confirm a suggestion once to start collecting that team. Its issues and
comments go to the project you confirmed; the Linear project an issue is filed
under is kept with it:

```sh
mewla watch projects --project <id>      # shows linear.suggested
mewla watch project link-linear --project <id> --team PER --confirm
```

## Problems

A few clear-cut problems are flagged immediately, without a model:

- **CI is red.** The latest CI run on the default branch failed. Watch also
  shows how long it has been failing.
- **Repeated daemon error.** The daemon reported the same problem (same kind
  and subject) at least five times within ten minutes in the last week.
- **Worker stalled or lost repeatedly.** One piece of Work lost or stalled its
  Worker at least twice in the last week.
- **Phone notifications are not being delivered.** The last three pushes that
  reached the push service all failed, at the send or in the delivery
  receipt (for example a missing push credential). A computer with no phone
  paired sends nothing and is not flagged. Whether a push was *worth* sending
  is never a rule: that is judgment's call.
- **The same notification sent over and over.** One kind, Session and text sent
  more than three times within ten minutes, delivered or not. Pushes the
  daemon held back are not counted.

### Problems and Work

A problem is tied to the Work that handles it, so the ones Brain already dealt
with stop asking for you:

- A problem is linked to Work when its rule's subject is that Work, when its
  signals happened in it, when its text names the Work id, or when the Work
  was opened for it (its objective names the problem id). Brain adds the
  rest while triaging: `mewla watch problem link <id> --work <work id>`.
- Work that is running makes the problem **fixing**. Work opened for the
  problem, or the stalled Work itself, makes it **fixed** once Brain accepts
  it after the problem's last signal. Work the problem merely happened in
  never fixes it.
- A problem that returns after being fixed is **recurred**. A cancelled Work
  puts it back. Your `not` label always stands.

### Judgment

With Watch on, new signals are handed in batches to one agent run with no
tools. It does four things:

- **Places** signals that have no project, by what they say: messages to
  Brain, conversations in folders that were renamed or deleted, and Work whose
  turn never reached a transcript. When the evidence is weak it leaves them
  unplaced.
- **Says who wrote** a message in a Worker conversation: you, a Brain
  follow-up, or a routine continuation.
- **Raises problems**: it separates problems from noise, merges signals from
  different sources about the same thing (rule flags included), estimates the
  cost (your attention, or the project's health), and marks problems that look
  fixed. Each problem keeps its evidence, a one-paragraph reason and, when there
  is one, a concrete suggestion.
- **Judges notifications**: for each push it reads the text, the Work or thread
  it was about, whether you were already in the app or that Session, what you
  wrote around then, earlier pushes about the same thing and what you did with
  it, and decides whether it was worth interrupting you: `meaningful`, `noise`,
  `poorly_timed`, `duplicate` or `unclear`. For a push that was held back it
  says `missed` (you needed to know and nothing reached you) or `rightly_quiet`.
  A tap is evidence, not the verdict, and so is no tap. A pattern, such as one
  kind of push that is repeatedly noise, becomes an ordinary problem with what
  it costs you and one suggested change.

Before that, signals whose turn, Session or Work later turned up in a known
repository are placed without a model.

Judgment runs on an agent you have, signed in the way your own copy is,
without tools, hooks, MCP servers or a saved session. Claude Code
(`claude -p`, Sonnet), Codex (`codex exec`) and Pi (`pi -p`) can judge. A run
takes at most 300 signals or about 60,000 characters. A full batch runs at once; a partial one waits two hours, since the rules
above already flag severe problems at once.
There are at most 60 runs a day, and a failed run waits 10 minutes and retries
with a smaller batch. Nothing runs when there are no new signals. Signal text
is treated as data, never as instructions, and secrets are removed again
before it is sent. Each run's tokens and cost are logged.

#### Which agent judges

One row of `routing.md` in Brain's workspace picks it, like a Worker's route.
The Task cell starts with `Watch judgment`; the other cells are the executor,
model and reasoning, with the same ids as for Workers (`default` leaves the
choice to the agent):

```
| Watch judgment | pi | opencode-go/deepseek-v4.1-flash | low |
```

Watch reads the row strictly: the executor must be Claude Code, Codex or Pi
and be installed, the model one id, and the reasoning a level that executor
knows (Claude low–max, Codex none–ultra, Pi off–max). It reads the file again
whenever it changes. With no row, or one it can't use, Watch chooses by itself
and `mewla watch status` says why (`judgment.routing`): Brain's own agent
first, then the one that last worked, then Claude Code, Codex and Pi. A row it
can use is the only agent asked; if that run fails the failure is logged and
retried, never passed to another model. New workspaces start with a Claude
Code Sonnet row. The row picks who judges and nothing else: no tools, hooks,
saved session, redaction, batch, daily and cost limits change with it. Each
run's executor, model, tokens and cost (where the agent reports it) are logged.

```sh
mewla watch enable
mewla watch status                 # judgment: pending, runs, tokens and cost
mewla watch judge --batches 3      # judge now instead of waiting
mewla watch problems --rule judgment
mewla watch signals --source human --kind user_message
mewla watch signals --source notification --kind worker_done   # pushes, their outcome and verdicts
```

### Telling Watch it was right

Label a problem `real`, `not` (not a problem) or `later`, with an optional
note. `real` confirms it and `not` dismisses it. `later` hides it from the
brief for a week, or for as long as you name with `--for` (`90m`, `12h`, `1d`,
`2w`), and ends early if the problem shows new signals. Brain passes the time
when you give one ("tomorrow", 「下周再提」). Your labels and notes are given to
later runs, so they learn what you care about.

```sh
mewla watch problem label j_1a2b3c4d5e6f not --note "flaky runner, not our code"
```

## Reaching you

Watch writes to Brain the way a message from Telegram or another computer
does, and only while Watch is on. Brain decides what to do and what to tell
you; Watch never speaks in your conversation itself, and what it hands Brain
stays out of the thread. The rules live in Brain's `policies/watch.md`.

- **Triage.** New or changed problems go to Brain in one batch, after they
  have been quiet for half an hour or ten have piled up, at most every two
  hours. Brain closes noise (`not`, with a reason, marked as Brain's verdict
  so Watch doesn't learn it as yours), links problems to the Work already on
  them and leaves the rest. It doesn't message you.
- **Urgent.** A failed CI run on a default branch that is its workflow's latest
  run, or a problem costing 80 or more, whose last signal is under six hours
  old, is pushed at once, once per episode, and Brain tells you in a line.
  Starting the daemon or turning Watch on never pushes what was already
  urgent.
- **Morning brief.** The brief is an ordinary [Calendar](calendar.md) item
  called "Morning brief" (「早报」 in Chinese), named in the language your app is set to. The first
  time Watch is on, Watch creates it once: a daily reminder at the next 08:30
  in your phone's timezone (the phone tells the daemon; the computer's
  timezone if it never has). When it is due,
  Brain posts one message listing the three to five problems nobody is
  handling, one plain line each: what it is and what it costs. Answer by
  typing, such as "1 handle, 3 not a problem". Handle opens Work for
  it, not a problem labels it `not`, later labels it `later`. A problem a brief
  showed rests for three days; `later` rests until it gets new signals or its
  time (a week unless you named one) passes. With nothing to say there is no brief, and the phone shows no
  reminder of its own for this item.

  Change the time or repeat in Calendar (or `mewla calendar update`) and the
  brief follows; cancel the item and briefs stop for good, because Watch only
  creates it once. It stays a reminder: it can't be turned into another kind.
  Each occurrence briefs at most once, also across restarts. A daemon that was
  down when it was due still briefs up to three and a half hours late; after
  that the occurrence is skipped.

- **Language.** The app tells the daemon its language along with its
  timezone. Watch writes problem titles and reasons in it, and names the
  Morning brief item and the urgent push in it (English and Chinese so far;
  another language gets English there, while Brain still writes to you in
  yours). An app that doesn't report one falls back to the language of your own
  messages (Chinese is recognised), then English.

```sh
mewla watch triage --backlog       # hand Brain every problem still waiting, once
mewla watch brief                  # hand Brain the brief now, whatever Calendar says
```

## Storage

Everything lives in `~/.mewla/watch/`: `config.json` (the setting),
`projects.json`, `signals.jsonl` (append-only), `cursors.json` (how far each
source has been read), `problems.json`, `index.json` (which working directory
each turn, Session and piece of Work used), `annotations.json` (placements and
authors found after collection, and which signals judgment has seen) and
`judge-runs.jsonl` (each run's size, results, tokens and cost) and
`notify.json` (what Brain has been told, which brief occurrence went out last, whether the Calendar item
was created, and your timezone). Running a
collection pass again adds nothing that's already stored.
