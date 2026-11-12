# Brain and Work

Brain is the agent that leads the others. Tell it a goal and your limits once.
It splits the goal into **Work**, hands each piece to a **Worker**, reads what
comes back and decides the next step. You don't have to type "continue", and
it calls you only when a decision is yours.

## The words

| Term | Meaning |
| --- | --- |
| Brain | One long-running agent Session on your computer, running on the agent you chose (its **host**). |
| Work | One durable piece of the goal: an objective, a status and its history. It survives restarts. |
| Worker | An ordinary, visible Session that Brain started for a piece of Work. You can open it and take over. |
| Brain thread | Brain's conversation. It keeps its history when Brain moves to another agent. |

## Give Brain a goal

Brain's conversation is the app's home screen. Say the outcome you want and
anything Brain must not do:

> Ship atlas-notes v1.4 this week: fix the sync bug, tidy the settings copy,
> write release notes, post them to Notion.

Brain records each part as Work, starts Workers, and names the goal. Workers
run unattended, so read [Permission bypass risks](executors.md#permission-bypass-risks)
before you let Brain work on a machine with secrets.

The composer's **+** attaches a photo, a file, a screenshot of the computer or
a file on the computer. Typing `@` lists your installed agents, and `/` at the
start lists the slash commands of the agent Brain runs on.

## Follow the Work

**The state control.** Next to **Brain** in the header, it says how things
stand: "All quiet", "3 running", "2 need you". A Work with three Workers counts
as one. Tap it to open the tray.

**The tray.** Everything open, by what it asks of you: **Needs you**,
**Running** (queued and waiting Work sit here as quiet rows), **Today**,
**Idle** and **Done** (unread results first). A row with an agent's logo opens
that Session; a row with Brain's mark (a Work with several Workers, or none)
opens Work detail.

**The goal.** When Brain has set one, it is the tray's first line, with how
much has come back: "Ship atlas-notes v1.4 this week · 2 of 5 back". A new chat
starts without one. The app stops showing it once it is no longer current: all
its Work is done, everything has been back for 30 minutes, or nothing in it has
moved for 12 hours.

**Work cards in the conversation.** Each Work has one live card: its title, up
to three Workers (logo, name, what it does, time) and "N more". When the Work
reports again, earlier cards of it shrink to one line, so the thread never
fills with copies. Tap the title for Work detail, or a Worker for its Session.

**Results.** When a Work comes back, a result card arrives: the agent, the
title, when it finished, the summary and its next steps as buttons. Tapping a
next step sends it to Brain as your message. Once you open it or act on it, the
card becomes one line; tap the line to see the result again, with **Open
session**. A result that needs a decision is a request instead (below).

**Folded steps.** The searches, reads and commands Brain runs in a turn fold
into one "Worked · N steps" row. Tap it to see them. Session chats fold the
same way, but the row says what happened ("Read 3 files").

**Background tasks.** When a Claude Code subagent, background command or
monitor reports back, the chat shows a task card with its status, duration,
tool uses and tokens. Tap it for the full result.

**Sending is a queue.** Press Send and the message is yours to forget: it
appears in the chat with a small clock and goes to your computer as soon as it
can, again and again if the connection drops, even if you close the app and
come back. While the computer is offline the composer says "Sends when back
online". Messages in a row arrive in the order you sent them, and a message
that did arrive is never sent twice. While the agent is busy, the message goes
to it anyway, the way you would type into the CLI mid-turn: Claude Code queues
it, Codex takes it into the running turn, and Brain reads it between steps. The
clock turns into the normal message once the agent shows it. Only a message
your computer keeps refusing (for example, its session no longer exists), or
one whose delivery it could never confirm, shows **Not sent**, with **Send
again** and **Delete**.

**Connection.** The state control and the cat carry it: "Connecting…",
"Offline", "Pair again". Nothing is pinned over the chat. Open the tray while
offline for the reason ("Relay unreachable") with **Try again** and
**Details**. The app keeps the connection fresh by itself: a link that stops
answering is replaced within about half a minute, and a connection that was
healthy reconnects at once.

## Requests

Anything that waits for you is one **request**, shown in three places:

- a card in the thread of whoever asked: Brain's questions in Brain's thread,
  an agent's prompt in its Session (and in Brain's thread too when that Session
  works for Brain);
- a **Needs you** row in the tray;
- a [notification](notifications.md) when the app is closed.

Answer in any one of them and all three close at once: the card shrinks to one
line with your answer, the tray row goes and the count drops. A plain yes/no
gets **Approve** right on the tray row; anything else takes you to the card.
Several open requests in a row stack, oldest first. A request that passed
without an answer reads **Not answered**.

A Work that came back failed, with no result, or needing a decision is a
request too. Its tray row opens Work detail, where the card sits under
**Waits on you**.

Brain asks with buttons by setting a question on the Work:

```sh
mewla brain work update -id <work-id> -status waiting \
  -question "Keep both copies, or the newest edit?" -choice "Keep both" -choice "Newest wins"
```

## Work detail

Tap a Work card's title, a tray row with Brain's mark, or a Session's **⋯ ›
Open its Work**. Work detail shows the state ("Needs you", "N running" or
"Done", and since when), Brain's brief, and:

| Part | Does |
| --- | --- |
| **Waits on you** | The open request itself; answering here answers it everywhere |
| **Workers** | Each Worker; tap one for its Session |
| **Work file** | The Work's notes, to read or edit |
| **Ask Brain about this** | Back to Brain with the Work quoted in the composer |
| **Snooze 1 hour** | Takes it out of Needs you for an hour |
| **Stop** | After you confirm, closes its Workers and cancels the Work |
| **Reply about this Work** | Sends your message to Brain about this Work |

A Work started by a Calendar item also links that item.

Your reply goes to Brain as a message about that Work. You never have to close
finished Work: once Brain has read a result and nothing is pending, Mewla asks a
small model whether the Work is over and closes it if so. Brain reopens a Work
closed that way as soon as it acts on it again. A result no Brain has received,
or one Mewla could not judge, moves to Needs you after a day, so nothing sits
there forever.

## Who decides that Work is done

Brain, after it reviews the result, or you. A Worker saying "done", a process
exiting or a pane going quiet is evidence, not completion. When a Worker
reports, Brain reads the result and accepts the Work, sends a follow-up or
tries again. A Worker that disappears leaves its Work marked for review.

When you close Work yourself, that is final: Brain does not reopen it. From the
computer:

```sh
mewla brain work list --json                 # open Work
mewla brain work list --json -all -full      # include closed Work and objectives
mewla brain work list --json -id <work-id>   # one Work with its history
mewla brain work update -id <work-id> -status done
```

`work list -id` also shows `brain_work_transcripts`: for each Worker attempt,
the agent, its conversation id and the path of its transcript. The link stays
after the Worker and the Work are closed, so a past result can be traced back
to what the Worker did. Mewla records the path; the agent owns the file, and
Claude Code removes its own transcripts after about 30 days.

## Brain's menu

**⋯** at the right of the header:

| Row | Does |
| --- | --- |
| **Open terminal** | Brain's own Session, on its terminal. A prompt Brain is blocked on can be answered there. |
| **Browse workspace** | Brain's notes ([below](#brains-workspace)); a file opens in the preview |
| **Switch executor** | The agent Brain runs on ([below](#switch-brains-agent)) |
| **Model** | Brain's model, and **Effort**: **Low**, **Medium** or **High** |
| **New chat** | After you confirm, a fresh thread. Open requests stay open in it. |

A row that does not apply right now is dimmed. Earlier threads have no list: a
notification about one opens it read-only, with **Current chat** to return.

## Brain's memory of the chat

Brain looks up what you said or decided in its own chat, rather than guessing:

```sh
mewla memory search "不要用写死的规则"               # exact phrase first, then newest
mewla memory search handoff --from user --since 30d  # only your messages, last 30 days
mewla memory show <id> --around 5                    # one message and the 5 on each side
```

Search covers visible text only: your messages, Brain's replies and Work result
cards, including messages you sent while Brain was busy. Text Brain's own
agent session fed back into the chat that you did not type (a compaction
summary, a forwarded Work event, a notification) is labelled `context`: it is
left out of `--from user`, and under `all` it ranks after real messages. Tool output and
thinking are not in the chat log, so they are never searched. Any phrase works,
including Chinese and partial phrases; one- and two-character queries are
matched literally. `--from` is `user`, `brain`, `context` or `all`, `--since` and `--until`
take a date, an RFC 3339 time or an age such as `30d`, and `--json` prints the
hits with their ids, times and Work ids.

The index is a file in Brain's state directory that Mewla builds from the chat
log and updates on each query; the first search on a long history takes several
seconds. Text that looks like a credential (known key prefixes, JWTs, private
keys, `KEY=value` secrets, long random strings) is masked before it is stored.
The chat log itself is unchanged.

## The cat

The cat at the left of the header is Brain on this computer. Tap it for the
computer page. What it does is Brain's real state:

| Cat | Brain |
| --- | --- |
| Naps | All quiet |
| Wakes and sits up | Connecting, or Brain is starting |
| Works | Brain's own turn is running |
| Delegates | Workers are running and Brain waits on them |
| Raises a paw | Something needs you |
| A short "done" | A Work came back done |
| Naps, greyed | Offline, must pair again, or Brain can't start |

It moves once when the state changes, then holds still. It never moves while
you scroll or type, and with reduced motion on it shows only a still.

Pick another pet in **Settings › Pet**: Dango (the default), Ash, Kami, Bit,
Doodle or Riso. The choice stays on that device (the phone and the web app each
keep their own).

## Worker routing

Brain picks the agent, model and reasoning level for every Worker. Before each
launch it reads `routing.md` in its workspace, a few lines of Markdown such as
"use Codex with high reasoning for refactors". It rewrites a line when you
state a preference, when a new model appears or when a choice went badly.
Upgrades never overwrite it. Mewla reads one row only: the one starting
`Watch judgment`, which picks the agent that reads [Watch](watch.md) signals.

A Worker launch looks like this:

```sh
mewla worker spawn -name "Fix flaky test" -executor claude \
  -model claude-opus-5-5 -reasoning high -cwd /path/to/repo -prompt "..."
```

Without `-executor`, the Worker uses Brain's own agent. Mewla maps `-model` and
`-reasoning` to each client's flags:

| Executor | `-model` becomes | `-reasoning` becomes |
| --- | --- | --- |
| `codex` | `--model` | `-c model_reasoning_effort=...`: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `ultra` |
| `claude` | `--model` | `--effort`: `low`, `medium`, `high`, `xhigh`, `max` |
| `pi` | `--model provider/id` | `--thinking`: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` |
| `grok` | `--model` | Not supported |
| `agent` (Cursor) | `--model` | Not supported |
| `opencode` | Not supported; OpenCode 2 uses the model saved in OpenCode | Not supported |

A launch value replaces the same flag in the configured command. An executor
that cannot take an option fails the launch with an explanation rather than
dropping it; empty values keep the client's default. Name executors after
clients, not capabilities: no `codex-high`.

## Watch and steer Workers

Workers are in the tray like any other Session: open one, read it as Chat or
Terminal, type into it. From the computer:

```sh
mewla worker list --json
mewla worker status -id <session-id> --json
mewla worker capture -id <session-id> --json      # transcript
mewla worker send -id <session-id> --work-id <work-id> -text "Also update the tests"
mewla worker receipt -id <session-id> --work-id <work-id> --turn-id <turn>
mewla worker close -id <session-id>
```

`--work-id` delivers the follow-up as part of that Work. If a send's outcome is
unknown, the input may still have arrived; `mewla worker receipt` checks
without sending it again. Closing a Worker, or accepting its Work, stops the
processes that Worker started. Your own Sessions and Brain are never cleaned
up this way.

## Brain's workspace

Brain keeps plain-file notes under `~/.mewla/brain`; `mewla brain workspace`
prints the path. **Browse workspace** in Brain's menu opens them in the app.

| File | Holds |
| --- | --- |
| `routing.md` | Which agent, model and reasoning for which kind of task |
| `profile.md` | Your background and preferences |
| `memory.md` | Durable facts and decisions |
| `current.md` | A short handoff of the active work |
| `AGENTS.md` | Brain's standing instructions, partly managed by Mewla |
| `worklog/` | Brain's reports and handoffs |

Video and audio files in the workspace play in place, through the same stream
as a file in a reply; that needs Brain's agent to be running.

Edit them freely. Mewla refreshes only its own marked blocks in `AGENTS.md`.
`mewla brain gc` reports when a note has outgrown its size budget.

## Switch Brain's agent

In the app, **⋯ › Switch executor** lists the installed agents with their
provider and account, the current one checked, then "Install more with mewla
deps". Pick one and Brain moves to it. From the computer:

```sh
mewla brain executors --json
mewla brain use claude
```

The thread and its history move with Brain. First run asks which agent Brain
uses when more than one is ready and keeps that answer. A start without a
terminal (a service, CI) never asks: Brain takes the first installed agent in
the order Claude Code, Codex, Cursor Agent, Grok, Pi, OpenCode, preferring one
`mewla doctor` verified as signed in over one it could not verify, and the daemon
log names it and the `mewla brain use` command that changes it.

When Brain has no running host, the state control reads "Can't start". The
tray says why: the agent is not installed, is not signed in, did not start, or
is open but never became ready (a sign-in or first-run prompt waiting in its
terminal). It offers **Use** and an agent's name (**Use Codex**) for an installed agent
that is signed in,
and **Switch executor** when the computer has another agent installed; with
none installed, `mewla deps` is the next step. Nothing needs a restart: Mewla
checks again every few seconds, starts Brain's host as soon as the agent is
installed or signed in, and starts it again after a reboot (a host that never
held a conversation restarts by itself; one holding a conversation Mewla cannot
resume is never replaced blind). Brain's pane starts in your login shell, so an
agent Mewla sees as signed out still starts: it may be signed in through that
shell. If it is not, it shows as open but not ready.

When the agent Brain runs on has no chat view, the thread says "Chat isn't
available" with **Open terminal** and **Switch executor**.

## Brain from elsewhere

- **Telegram**: your bot chat talks to the same Brain thread. See [Telegram](notifications.md#telegram).
- **Calendar**: a **Mewla action** runs as Work and posts back to the thread it came from. See [Calendar](calendar.md).
- **Extensions**: Brain and Workers can read Linear, Notion, GitHub and more, and change them with your consent. See [Extensions](extensions.md).
