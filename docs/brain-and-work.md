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
zen brain work list --json                 # open Work
zen brain work list --json -all -full      # include finished Work and objectives
zen brain work list --json -id <work-id>   # one Work with its history
zen brain work update -id <work-id> -status done
```

## Watch and steer Workers

```sh
zen worker list --json
zen worker status -id <session-id> --json
zen worker capture -id <session-id> --json      # transcript
zen worker send -id <session-id> --work-id <work-id> -text "Also update the tests"
zen worker receipt -id <session-id> --work-id <work-id> --turn-id <turn>
zen worker close -id <session-id>
```

`zen worker send` with `--work-id` hands the follow-up to the Worker as part of
that Work. If a send's outcome is unknown, the input may still have arrived;
`zen worker receipt` reads whether it was accepted without sending it again.

Closing a Worker, or accepting its Work, stops the processes that Worker
started. Your own Sessions and Brain are never cleaned up this way.

## Brain's workspace

Brain keeps its notes in a workspace under `~/.zen/brain`. `zen brain workspace`
prints the exact path.

| File | Purpose |
| --- | --- |
| `routing.md` | Which executor, model and reasoning to use for which kind of task |
| `profile.md` | Your background and preferences |
| `memory.md` | Durable facts and decisions |
| `current.md` | A short handoff of the active work |
| `AGENTS.md` | Brain's standing instructions, partly managed by Zen |
| `worklog/` | Brain's reports and handoffs |

You can edit these files. Zen refreshes only its own managed blocks in
`AGENTS.md` and never overwrites your text. `zen brain gc` reports when a note
has grown past its size budget.

## Switch Brain to another agent

```sh
zen brain executors --json
zen brain use claude
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
