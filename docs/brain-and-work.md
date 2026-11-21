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

**Share to Brain (Android).** Mewla is in the phone's share sheet, for one file
or several (pictures, video, audio, documents) and for text and links, and in
**Open with** for a file of any type: the entry a chat app such as WeChat offers
on a file as 「用其他应用打开」. Mewla opens on Brain's chat with the files as chips
above the composer, uploading at once, and a shared link or text added below
what you had typed. Nothing is sent until you press Send. A second share joins
the first. It goes to the computer you are using; while no computer is paired
or it cannot be reached, the share waits (a failed chip with Retry once the
computer cannot be reached). The composer holds eight files; the rest wait
for room. A share with nothing Mewla can use says so once, where attachment
errors show. iOS, Mewla for Mac and the web do not receive shares yet.

## Follow the Work

**The state control.** Next to **Brain** in the header, it says how things
stand: "All quiet", "3 running", "2 need you". A Work with three Workers counts
as one. Tap it to open the tray.

**The tray.** One flat list of everything you asked for, by what it asks of
you: **Needs you**, **New** (a result is back and you have not opened it),
**Running** (every running Work, the one that changed last on top; queued and
waiting Work sit at its end as quiet rows), **Today**, **Idle**, and **Done**,
one row that opens in place. Its title is its counts, "2 need you · 3 new ·
17 running": tap one to see only those rows, tap it again for all. While two or
more projects have open Work, chips under the title filter by project.

Each Work row reads as a model wrote it, in your language: a short name for
the thing you want and one line on where it stands ("Cause found; fixing the
router session"), with how long since it last changed. The progress judge
writes both, and the project, in the same pass that judges progress; until it
has, the row shows Brain's title. A judge that cannot answer leaves the last
text. Brain's own notes on a Work (what it waits for, its next step) are
Brain's shorthand and never show: a Work Brain parked reads, for example,
"无需你操作：等明早 11:17 日志清理跑完，再核对修复效果", in your local time, and
says "Nothing needed from you" until the judge has said what it waits for. Ids,
hashes and UTC times never reach a row. Several Workers on one Work are still one row ("3 Workers · …"). Tap a
row for Work detail; tap the Worker's logo, with its ring, to open its Session
directly.

Work detail reads top down: Brain's question when it asks one, **Now** (the
row's line), **Next**, **Outcomes**, **So far** (each line the row has read,
with when), **Asked** (your own messages to Brain that asked for it, as the
judge picked them) and **Workers**, each opening its Session. Under ⋯:
**Rename** and **Move to project** set the title or project as you write it,
and the judge never changes it again (clear it to hand it back); **Merge
with…** shows the Work's row under another Work, with its Workers, until you
unmerge it. Merging changes only the list, never the Work itself. Brain does
the same from the command line: `mewla brain work show-set -id <work>
-title|-project|-merge-into <value>`.

**The goal.** When Brain has set one, it is the tray's first line, with how
much has come back: "Ship atlas-notes v1.4 this week · 2 of 5 back". A new chat
starts without one. The app stops showing it once it is no longer current: all
its Work is done, everything has been back for 30 minutes, or nothing in it has
moved for 12 hours.

**Work cards in the conversation.** Each Work has one live card: its title, up
to three Workers (logo, name, what it does, time) and "N more". When the Work
reports again, earlier cards of it shrink to one line, so the thread never
fills with copies. A card with one Worker reads as the Work's tray row: the
judge's name and line beside its Worker's logo and ring. Tap the title for Work
detail, or a Worker for its Session.
Work detail lists every Worker that joined the Work, finished ones too; in the
tray a Work with several Workers is one row reading "N Workers".

**How far a running Work has come.** A running Work has a ring round its
Worker's logo, filled as far as a model judges the Work has come, not as its
Worker reports. No number is written; a screen reader speaks it, rounded to 5.
When the Work starts, the judge turns the brief into a short weighted list of
outcomes, things that will be true when it is done, such as "fix merged and
released" or "review posted", and marks each from evidence: a commit, a
passing run, a file written, a posted review.

- **It only moves forward**, unless an outcome is reopened (Brain sent the Work
  back, or a later check failed). Then the ring goes back once, in amber, and
  Work detail says why.
- **It never fills to 100 %.** When every outcome is done, the Work is in its
  last stretch (the Worker reporting, Brain reading the result), and the ring
  closes in green.
- **No ring** while Brain asks you something on it, while Brain reviews what
  its Worker sent, or for a Work with no finish line. A Work Brain parked on
  something else (a review, a date, a release) keeps its ring: it asks nothing
  of you.
- **Tap the Work** to see its outcomes and which are done. Outcomes are
  written in your language; one first written in another is said again in
  yours, once. An outcome not yet done that Brain's later message replaced or
  withdrew leaves the list; a done one never does.

The judge runs only on new evidence (a progress report with new words, a turn
ending, Brain's next message to the Worker, Brain parking or closing the Work or
changing what it waits on or asks you), never on a timer alone. Reports alone
move it at most every ten minutes per Work; after a turn ends or a send-back,
within seconds. A report that repeats the last one is not evidence, and the evidence it
last judged is kept on the Work, so a restart of the computer's Mewla judges
nothing again. The same call writes the row's title, line and project, in the
language you write to Brain in; a new Work is named once when it appears, and a
closed one is read once more so its row says how it ended. The judge has no daily
cap: what it costs follows from judging only new evidence, one call per Work
and the model you route, and `mewla brain context --json` shows the last 24
hours (`progress_judge`: judgments, Works, dollars). The
`Work progress` row of `routing.md` picks its model (see
[Worker routing](#worker-routing)). Each judgment is recorded on the Work as a
`progress.judged` or `progress.back` event (`mewla brain work list -id`).

**Results.** When a Work comes back, a result card arrives: the agent, the
title, when it finished, the summary and the next steps it truthfully offers,
as buttons: the pull request it opened, **Review changes** when its project
has uncommitted changes, the files it names, and the Work's worklog. A result
that names none has none. Once you open it or act on it, the
card becomes one line; tap the line to see the result again, with **Open
session**. A result that needs a decision is a request instead (below).

**Folded steps.** The searches, reads and commands Brain runs in a turn fold
into one row that says what happened ("Ran 3 commands, read 2 files"), with
the files it touched beside it. Tap it to see the real rows. Session chats
fold the same way.

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

A Work is a request only when Brain asks you something on it. Its tray row
opens Work detail, where the question and its answers come first, Brain's pick
tagged, and the reply box sits at the bottom; the brief, outcomes and Workers
fold under **Details**. A Work Brain left waiting with nothing asked is a quiet
**Waiting** row, never under Needs you, and its page's box reads **Tell Brain
about this Work**, not Reply.

**Who a request is for.** Needs you holds only what you can act on: a question
Brain asked you, a prompt Brain handed to you, a sign-in or approval only you
can give, and a Trigger whose check keeps failing or that paused itself. A
Worker that Brain started reports to Brain, not to you: when it stops on a
decision of its own, Brain decides, and asks you only through its Work's
question. Until then the Work reads Reviewing, and nothing for that report
reaches you: no card, no tray row, no count, no notification. A result Brain
has not decided, a reply Brain has not acted on and a Trigger firing Brain has
not handled are Brain's too; after a day Mewla wakes Brain for an undecided
result, and after two hours it hands Brain back a Work it parked with nothing
asked and nothing named to wait on. A wait Brain names (`-wait-for` a computer,
a pull request, a Worker's next commit), a Trigger's Work and a Work with a
live Worker are Brain's own and stay quiet. A wait Brain names has an end
(`-until`, else a day); once a day at most, Mewla hands Brain every Work whose
wait has run out in one list, and Brain closes each, does the step, or waits
again until a time. A finished Work closes: a later step gets its own Work, a
calendar item or a question. A prompt on a Worker's terminal that Brain cannot answer (a sign-in,
an approval outside the brief) still comes to you, and a Session you started
yourself asks you directly.

Brain asks with buttons by setting a question on the Work:

```sh
mewla brain work update -id <work-id> -status waiting \
  -question "Keep both copies, or the newest edit?" -choice "Keep both" -choice "Newest wins" \
  -recommend "Newest wins"
```

`-recommend` names the choice Brain recommends, word for word; the app tags
that answer "Brain's pick". It must be one of the choices, and a new question
starts without one. `-status waiting` without `-question` asks you nothing: the
Work shows as Waiting, and, unless `-wait-for` names what it waits on, the
command's answer tells Brain so. Such a wait is Brain's own: your reply on that
Work reaches Brain as a message about it and does not end the wait; Brain
decides whether it still holds. Only a Work that asks waits on you, and your
answer ends that wait. Brain can ask at any time, also while a Worker still
holds the Work (a Worker stopped on something only you can do): the question
goes on the card and the Worker keeps its turn.

## Work detail

Tap a Work card's title, a tray row with Brain's mark, or a Session's **⋯ ›
Open its Work**. Work detail shows the state ("Needs you", "N running" or
"Done", and since when), what Brain asks first when it asks something, and:

| Part | Does |
| --- | --- |
| **Question** | What Brain asks, with its answers; answering here answers it everywhere |
| **Details** | Folded while Brain asks: what it is doing, the brief (four lines until tapped), outcomes |
| **Workers** | Each Worker; tap one for its Session |
| **Work file** | The Work's notes, to read or edit |
| **Ask Brain about this** | Back to Brain with the Work quoted in the composer, below anything you had typed there |
| **Snooze 1 hour** | Takes it out of Needs you for an hour |
| **Stop** | After you confirm, closes its Workers and cancels the Work |
| **Reply about this Work** | Sends your message to Brain about this Work |

A Work started by a Calendar item also links that item.

Your reply goes to Brain as a message about that Work. You never have to close
finished Work: once Brain has read a result and nothing is pending, Mewla asks a
small model whether the Work is over and closes it if so. Brain reopens a Work
closed that way as soon as it acts on it again. A result that waits a day with
no decision wakes Brain once to settle it, so nothing sits there forever; it
never becomes a Needs you row on its own.

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
Upgrades never overwrite it. Mewla reads three rows itself: `Watch judgment`,
which picks the agent that reads [Watch](watch.md) signals, `Work progress`,
which picks the agent that judges how far a running Work has come, and
`Scheduled action`, which picks the executor, model and reasoning of a
[Mewla action](calendar.md#mewla-actions) that names none of its own. Without
a valid judgment row, Mewla uses Brain's own agent (Claude Code: Sonnet at low
reasoning); without a `Scheduled action` row, an action runs on Brain's own
executor and model.

```markdown
| Work progress | claude | sonnet | low |
| Scheduled action | claude | sonnet | low |
```

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
| `agy` | `--model` (an id from `agy models`) | Not supported (the level is in the id) |
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
processes that Worker started. A closed Work's Worker, and a finished
scheduled run's Session, are closed for you once they sit idle at their
prompt, even when the Worker never reported done. `worker close` needs
`--force` only for a Session that is still at work. Your own Sessions and
Brain are never cleaned up this way.

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
the order Claude Code, Codex, Cursor Agent, Grok, Pi, OpenCode, Antigravity (last: it has no hooks or approval handling), preferring one
`mewla doctor` verified as signed in over one it could not verify, and the daemon
log names it and the `mewla brain use` command that changes it.

When Brain has no running host, the state control reads "Can't start". The
tray says why: the agent is not installed, did not start, or is open but never
became ready (a sign-in or first-run prompt waiting in its terminal; when its
own sign-in check also says signed out, the tray says to sign in). It offers **Use** and an agent's name (**Use Codex**) for an installed agent
that is signed in,
and **Switch executor** when the computer has another agent installed; with
none installed, `mewla deps` is the next step (Mewla for Mac says "No agent on this Mac" in Brain's
thread with Install Claude Code and Install Codex, each its own `mewla deps -agent` in a terminal
Session that installs, signs in and moves Brain to that agent). Nothing needs a restart: Mewla
checks again every few seconds, starts Brain's host as soon as the agent is
installed, and opens it again once the agent is signed in (an agent's own
sign-in screen does not notice a sign-in made in another terminal). Mewla never
holds Brain back because an agent's sign-in check says signed out: Codex on an
API key or its own model provider, or a sign-in that lives in your login shell,
reads as signed out there and runs. If it really cannot run, it shows as open
but not ready.

When Brain's Session is gone (a reboot, a crash, an upgrade), Brain starts again
in its conversation when its agent can resume it. When it cannot (no
conversation was recorded, or the agent cannot resume it), Brain starts a new
conversation from its workspace notes and Chat shows **Brain restarted** once:
the earlier conversation couldn't be resumed.

When the agent ends right after it starts, Chat and the tray show how it ended:
its exit code and the last lines it printed. Mewla then waits two minutes before
it starts that agent again by itself, and stops starting it once it has ended the
same way twice in a row; `mewla brain use`, or choosing the agent in
the app, tries again at once and prints the same reason if it ends again.

A Worker whose agent ends right after it starts fails `mewla worker spawn` with
how it ended (exit code and last output) instead of answering ok, and its Work
says why it was cancelled. A Worker report Mewla could not record (for a turn
that was replaced, say) is logged, shows on the Worker as **Report not
recorded** with the reason, and is kept on its Work with what the Worker said.
`mewla brain use` waits up to a minute for the agent to answer and reports a
slow start as started, not failed.

When the agent Brain runs on has no chat view, the thread says "Chat isn't
available" with **Open terminal** and **Switch executor**.

## Brain from elsewhere

- **Calendar**: a **Mewla action** runs as Work and posts back to the thread it came from. See [Calendar](calendar.md).
- **Extensions**: Brain and Workers can read Linear, Notion, GitHub and more, and change them with your consent. See [Extensions](extensions.md).
