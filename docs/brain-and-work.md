# Brain and Work

Brain is the agent that leads the others. You give it a goal and your
boundaries; it splits the goal into **Work**, hands each piece to a **Worker**,
reads the results and decides what happens next. You do not have to type
"continue".

## The words

| Term | Meaning |
| --- | --- |
| Brain | One long-running agent Session on your computer, running on the executor you chose (its **host**). |
| Work | One durable piece of the goal: an objective, completion criteria and a status. It survives restarts. |
| Worker | An ordinary, visible Session that Brain started for a piece of Work. |
| Brain thread | Brain's conversation. Switching Brain's executor keeps the thread and its history. |

Work moves through `queued`, `running`, `waiting`, `blocked`, `done` and
`cancelled`.

## Give Brain a goal

Open **Brain** in the app and describe the outcome you want and anything Brain
must not do. Brain records the Work it creates as cards in its timeline. Each
card stays one card as the Work progresses, so repeated updates do not pile up.

Brain picks the executor, model and reasoning level for each Worker from its
`routing.md` guide; see [Worker routing](executors.md#worker-routing). Workers
appear in **Sessions** like any other Session, and you can open one, read it as
Chat or Terminal, and type into it yourself.

When a Claude Code background task reports back (a subagent, a background
command or a monitor), Chat shows a task card rather than a message from you.
The card shows the task, its status (done, failed or stopped), and its
duration, tool uses and tokens when Claude reports them. Tap it to read the
full result.

Each piece of Work shows up in the Brain chat as a card with its state:
a green check for Ready, a blue spinning arc while it runs, an amber triangle
for a warning, a crossed box with the cause when it failed, and a dashed ring
when it is blocked or waiting. Work that needs you has a red "Needs you" label
and a dark outline. Red always means "needs you", never "failed".

A Work's card sits in the chat where its newest result came back, so a Work
that reports again moves down to the moment it happened.

All of Brain's current Work is in one place. On a phone, the line under the
title ("6 need you · 2 running · 2 back") opens the list. On a wide screen,
the Work column to the right of the chat shows it, grouped by what each Work
asks of you: Needs you, Running, Back and Waiting. Tap a card to open its
Session, or its details when the Session has closed. Work that is closed
leaves the list.

When one request turns into several Works, Brain names the goal with
`mewla brain objective set "<goal>"`. The app shows it under the title with
how much has come back, for example "Ship atlas-notes v1.4 this week · 2 of 5
back". `mewla brain objective clear` removes it, and a new chat starts
without one.

The steps Brain takes in a turn (searches, reads, commands) fold into one
"Worked · N steps" line. Tap it to see them.

The cat shows what Brain is doing, in one place at a time:

- **Asleep in the seal:** Brain is idle.
- **One eye open:** the app is connecting.
- **Walking:** Brain is working on your message.
- **Sitting on a card, ears up:** that Work needs your input.
- **Sitting by moving dots:** Workers hold delegated Work.
- **Lying beside a parcel:** a result is waiting for you to read.
- **Grey seal:** your computer is offline.
- **Empty seal:** no computer is paired yet.

The menu (☰, or the sidebar on a wide screen) holds Calendar, Plugins,
Skills, Stats, Resources and Settings.

The cat stays still when your device asks for reduced motion.

Workers run unattended. Read
[Permission bypass risks](executors.md#permission-bypass-risks) before you let
Brain work on a machine with secrets.

## Who decides that Work is done

Only Brain or you can mark Work done. A Worker that says it finished, a process
that exits or a pane that goes quiet is evidence, not completion. When a Worker
reports, Brain reads the result and either accepts the Work, sends a follow-up,
or tries again.

If a Worker disappears or stops reporting, its Work is marked as needing review
instead of being treated as finished.

You can do the same from the computer:

```sh
mewla brain work list --json                 # open Work
mewla brain work list --json -all -full      # include finished Work and objectives
mewla brain work list --json -id <work-id>   # one Work with its history
mewla brain work update -id <work-id> -status done
```

## Watch and steer Workers

```sh
mewla worker list --json
mewla worker status -id <session-id> --json
mewla worker capture -id <session-id> --json      # transcript
mewla worker send -id <session-id> --work-id <work-id> -text "Also update the tests"
mewla worker receipt -id <session-id> --work-id <work-id> --turn-id <turn>
mewla worker close -id <session-id>
```

`mewla worker send` with `--work-id` hands the follow-up to the Worker as part of
that Work. If a send's outcome is unknown, the input may still have arrived;
`mewla worker receipt` reads whether it was accepted without sending it again.

Closing a Worker, or accepting its Work, stops the processes that Worker
started. Your own Sessions and Brain are never cleaned up this way.

## Brain's workspace

Brain keeps its notes in a workspace under `~/.mewla/brain`. `mewla brain workspace`
prints the exact path.

| File | Purpose |
| --- | --- |
| `routing.md` | Which executor, model and reasoning to use for which kind of task |
| `profile.md` | Your background and preferences |
| `memory.md` | Durable facts and decisions |
| `current.md` | A short handoff of the active work |
| `AGENTS.md` | Brain's standing instructions, partly managed by Mewla |
| `worklog/` | Brain's reports and handoffs |

You can edit these files. Mewla refreshes only its own managed blocks in
`AGENTS.md` and never overwrites your text. `mewla brain gc` reports when a note
has grown past its size budget.

## Switch Brain to another agent

```sh
mewla brain executors --json
mewla brain use claude
```

The conversation and its history move with Brain. In the app, **New Chat**
starts a fresh Brain thread.

## Brain from other places

- **Telegram**: messages in your bot chat go to the same Brain thread. See
  [Telegram](notifications.md#telegram).
- **Calendar**: a scheduled action runs as Work and posts its result back to the
  Brain thread it came from. See [Calendar](calendar.md).
- **Plugins**: connected services let Brain and Workers read, and with your
  permission change, Linear, Notion, GitHub and more. See [Plugins](plugins.md).
