# Sessions

A **Session** is one agent or shell running on your computer. Mewla starts it,
lists it and lets you read or drive it from the phone. Mewla's session host
keeps it running while Mewla restarts or updates, and the same Session is there
when you sit down at the desk ([`mewla attach`](#at-the-desk-mewla-attach)).

## Find a Session

Tap the state control next to **Brain** to open the tray. It lists your
Sessions by what they need: **Needs you**, **Running**, **Idle** and **Done**.
Running rows are ordered by when you last opened them, so the Session you live
in is always the first Running row. Back from a Session returns to the open
tray, so hopping between Sessions is Back, then a row.

**All Sessions**, at the bottom of the tray, is the full list grouped by folder.
Tap a folder's name to fold it. Long-press a row to select several and
terminate them together (on the web, right-click a row).

Brain's Workers are ordinary Sessions and appear here too. A Work with several
Workers is one row with Brain's mark and "N Workers"; it opens Work detail (see
[Brain and Work](brain-and-work.md)).

## Start a Session

Tap **+** in the tray (or on All Sessions; on the web, press N), pick a folder
and tap what to run:

| Choice | Runs |
| --- | --- |
| Claude, Codex, Cursor, Grok, Pi, OpenCode | That agent CLI, using the command from [Agents and models](executors.md) |
| Shell | Your login shell |
| DSH | The DSH harness, opened as a web page rather than a terminal |

**Advanced** lets you set any command (for example `claude --model opus`) and a
window title. The Session starts on the current computer.

The agent must already be installed and signed in on the computer; Mewla never
signs in for you.

## Chat and Terminal

A Session's header has a **Chat / Terminal** switch. The choice is remembered
per Session. What you type in either goes to the same terminal.

- **Chat**: the structured conversation, with your messages, the agent's
  replies, tool calls, plans and pictures. A run of finished tool calls folds
  into one row that says what it did ("Read 3 files"); tap it for the calls. A
  call that failed is folded like any other and the row says so; a call that is
  running or waiting on you stays on its own row, and so does a subagent, which
  reads as the task it was given. On Claude, tap a subagent to see the calls it
  made, folded the same way, with how many there were, how long it took and
  whether it is still working. Chat is available for Claude, Codex, Cursor,
  Grok, Pi and OpenCode.
- **Terminal**: the live terminal, for any command, a Shell included. The key
  bar adds Esc, Tab, Ctrl (armed for one key), ⌃C, ⌃D, ⌃B and arrows that
  repeat while held. Scroll up for **Search history**, **Previous prompt**,
  **Next prompt** and **Scroll to bottom**. Tap a link to open it in the
  browser, or a file path to preview the file.

When a Session is blocked on a prompt the chat cannot answer, it opens on the
Terminal by itself.

### Copy from the Terminal in a browser

Select text with the mouse and press **Cmd+C** on a Mac, or **Ctrl+C** or
**Ctrl+Shift+C** on Windows and Linux; the text goes to your computer's
clipboard. With nothing selected, Ctrl+C still interrupts the program (on a
Mac it always does), and right after a Ctrl+C copy the next Ctrl+C interrupts
again. A program that uses the mouse, such as `tmux`
with `mouse on`, `vim` or an agent's interface, keeps plain drags for itself:
hold **Shift** (**Option** on a Mac) while you drag to select instead. What
`tmux` copies when you select with it, and what a program copies with the
terminal's clipboard code (OSC 52), reaches the browser's clipboard too, on
`localhost` or an HTTPS address; programs inside `tmux` need `set -g
set-clipboard on` for that. Mewla never lets a program read your clipboard.

### When an agent is waiting for you

A prompt the agent stops on becomes a **request**: a card in the Session's
chat, a **Needs you** row in the tray and a notification. If the Session works
for Brain, Brain's chat shows the same card. Answer in any of them and all of
them close at once: the card shrinks to one line with your answer, the tray row
goes and the count drops. A plain yes/no gets **Approve** right on the tray
row; anything else takes you to the card.

Every card is read from the prompt the agent has open in its terminal and is
answered by Mewla, which checks the screen still shows that same prompt before
it presses a key. A prompt Mewla cannot answer this way has no card: the
Session is a Needs you row and opens on its Terminal.

**Claude's questions** (AskUserQuestion) come one at a time, as in Claude's own
prompt. Tapping an option on a single-choice question moves on to the next.
Where a question allows several options (**Choose any**), tick any and tap
**Next**. **Other…** takes your own words. **Review** lists every answer; tap
one to change it, then **Submit**. Mewla answers by pressing keys in the
Session's terminal, so it needs to see the whole question there: when the
terminal is shorter than the question (a long multi-question prompt, a keyboard
open over it), sending says the window is too short. Make it taller or close
the keyboard and send again.

**Permission, plan, startup and approval prompts** (run this command, accept
this plan, trust this folder, a Codex approval) show the prompt's own heading,
what it asks about, and one choice per numbered option, labelled as the agent
labels it.

When Claude, Codex or OpenCode asks to run a command or change files, and Mewla
started that Worker, the card names the command and offers the agent's own
options (Allow or Deny; OpenCode also offers Always allow). Your choice answers
the agent directly, and a request that was already answered, or that belongs to
another conversation, is refused.

A prompt that passed without your answer (answered in the Terminal, replaced
by another, or timed out) reads **Not answered**. If Mewla cannot send your
answer, it says so and the card stays, so you can try again.

### Copy and edit a message

Tap one of your messages for **Copy**, **Select Text** and **Edit**. Code blocks
have their own **Copy**. Copy gives the text as written, Markdown included.

Messages are read as standard GitHub-flavored Markdown on every platform:
headings, quotes, nested and numbered lists, task lists, tables,
strikethrough, links and bare URLs, HTML entities such as `&amp;`, images,
code blocks and Mermaid diagrams. A reply that is still arriving never shows
half-typed syntax: an unfinished `-`, `1.` or `>` waits for its text.

**Edit** puts the message back in the composer. What sending does depends on
the agent:

| Agent | Sending an edited message |
| --- | --- |
| Claude Code, Codex, Pi | Steps the agent's conversation back to that message, as the agent's own message-editing gesture does, and sends the text from there. The later turns leave the chat. Files the agent changed stay as they are. |
| Every other agent, and Brain | Sends the text as a new message. Nothing earlier changes. |

Mewla rewinds only when the agent is idle and its own message list is showing
the message you chose. If it cannot prove that, nothing is rewound or sent, and
the text waits in the composer as a new message you can still send. Brain's own
thread is never rewound.

A message that could not be delivered shows **Not sent**, with **Send again**
and **Delete**.

### Files and links in a reply

Tap a file path in a reply, a tool row or a changed-files list to preview it
without leaving the chat: Markdown and text as text, images you can zoom,
PDFs, video (mp4, m4v, mov, webm) and audio (mp3, m4a, wav, ogg, opus, flac).
Playback streams from the computer, so a long recording starts at once and
seeking does not download the whole file. Every file has **Download**; text
files (Markdown, code, txt, JSON) also have **Copy**, which copies the whole
file.

Web addresses, with or without `[text](…)`, open in your browser, including one
that sits right beside Chinese text or punctuation, and email addresses open
your mail app. A link that names a file, such as `[report](report.pdf)` or
`[app](./out/app.apk)`, opens that file as above. Inside a Markdown file you
opened, a link to another file is relative to that file. Links that can run
code (`javascript:`, `data:`) never open.

A file that is not really what its name says (an `.mp4` that is not an MP4
container, say) is not played. A codec the device can't decode, such as HEVC
in some browsers, can't be played there; download it instead.

## The Session's menu

**⋯** in a Session's header holds:

| Item | Does |
| --- | --- |
| **Changes** | [Git review](#git-review) for the Session's repository, with the count of changed files |
| **Files** | The Session's [files](#files) |
| **Model & Effort** | The model and effort for this Session only |
| **Open Web** | A DSH Session's web page |
| **New Terminal** | A Shell in the same folder and on the same computer |
| **Rename** | The Session's name |
| **Open its Work** | Work detail for the Work this Worker does |
| **Terminate** | Stops the Session, after you confirm |

## Git review

**⋯ › Changes**, or the changed-files card in the chat, opens the Session's
repository:

- **Changes** lists the files in **Staged** and **Not staged**, with their
  added and removed lines. The branch pill opens **Branches**; beside it, how
  many commits wait to be pushed.
- Tap a file for its diff. Under the hunks: **Ask {agent}** (for
  example **Ask Claude**) about the change, **Discard** (after you confirm) and
  **Stage** or **Unstage**.
- **Commit N files** opens the commit sheet.
- **History** lists the commits; tap one to read it.
- Git's **⋯**: **Refresh**, **Browse files**, **Fetch**, **Pull**, **Push**,
  **Copy path**, **Copy branch**, **Undo last discard** and **Discard all
  changes**.

## Files

**⋯ › Files** browses the Session's folder: folders first, then files with
their size. A file opens in the preview, as a file in a reply does.

## At the desk: `mewla attach`

```sh
mewla attach            # list the live Sessions and pick one
mewla attach brain      # go straight to one by name
mewla attach --all      # every Session at once, as a live grid to choose from
mewla host status       # the session host's Sessions in full
```

Ctrl-\ detaches; the Session keeps running. In the grid, Ctrl-] goes back from
a Session to the grid. A Session still running in `tmux` from an earlier Mewla
opens in `tmux`, which detaches with its own prefix key.

On Linux with a systemd user session, the session host runs as a user service
of its own (`mewla-session-host-…`), not inside the terminal or service that
started Mewla. When the computer runs out of memory and the kernel ends one
process, systemd does not stop the host and the Sessions with it; closing the
terminal Mewla was started from does not either. Without a systemd user
session the host starts directly.

Sessions an earlier Mewla started in `tmux` move to the session host when their
agent is idle; see [Updating from Mewla 0.2](install-daemon.md#updating-from-mewla-02).

## How Mewla follows each agent

Where an agent offers a way to be followed, Mewla uses it instead of reading
the screen or the agent's files:

| Agent | Followed through | Messages and Stop |
| --- | --- | --- |
| Claude Code | Its hooks, set when Mewla starts it | Typed into the Terminal; the hook confirms delivery |
| Codex | An app-server beside the Terminal, set when Mewla starts it; the conversation is read from the thread Codex announced, and the Status sheet asks the same app-server (account, usage limits, model, approvals, sandbox, token use) | Sent to Codex directly |
| OpenCode | OpenCode's background service: one event stream for all Workers, the session id chosen at launch | Sent to the service directly; a message to a session OpenCode has not created yet (its first) is typed |
| Pi | A small extension Mewla loads into each Pi it starts (`pi -e`), which reports Pi's turns over a local socket; the session file is chosen at launch | Handed to Pi by the extension; Stop aborts the running turn |
| Cursor | The chat is created before Cursor starts and resumed, so its transcript is known from the first moment; turns are read from the transcript and the screen | Typed into the Terminal |
| Grok, dsh | The agent's transcript and the screen | Typed into the Terminal |

A Worker that Mewla did not start, or that started with its own settings, is
followed from the transcript and the screen, and the app offers only what that
supports. For Codex that is the one case still found by looking: a Codex you
opened by hand has no app-server of Mewla's, so its conversation is matched by
folder, start time and the files it holds open, and its Status sheet says to
type `/status` in the Terminal. A Codex that Mewla started is not looked for
at all: until it announces its thread (a moment after it starts) there is
nothing to read, and only if it has not announced one after 30 seconds is its
transcript searched for.

Folder trust follows the same rule. Codex is trusted for a Worker's folder by
the Worker's own command line, so Mewla leaves your Codex config alone. Claude Code
has no per-launch way to do that, so for Claude Workers Mewla sets that one
folder's `hasTrustDialogAccepted` in Claude's own config, which is the key
Claude itself names for it. A Claude Brain host gets the same for the Brain
workspace, so a first launch on a new machine does not wait at the dialog.

Cursor and Grok can only be hooked through the user's own global
configuration, and Mewla never writes to it. They stay on the transcript and the
screen, and so does anything an agent offers only through such a file.

## `tmux` inside a Session

Sessions run in the session host, not in `tmux`, so a `tmux` command an agent
runs, `tmux kill-server` included, cannot end them. Worker and Brain Sessions
point `TMUX_TMPDIR` at their own scratch directory, so a plain `tmux` there
starts a server of its own.

## What the daemon costs while idle

The daemon watches your Sessions by listing them and reading the process table,
not by attaching to them. With nothing happening it settles to under 1% of one
CPU core:

- The process table is read straight from `/proc` on Linux, from the system's
  process list on Windows and through `ps` on macOS, and again only when a
  Session printed, appeared or went away, or when it is five seconds old.
- A Session's screen is read again only when the session host says it printed,
  or when the last read is 30 seconds old.
- After three quiet seconds the Session list is checked every two seconds
  instead of every half second; the first change brings the fast cadence back.

## Services

Agents start dev servers, previews and APIs. **Services**, in the tray's **⋯**
menu (and All Sessions' **⋯**), lists the ports they listen on, by project:

| Source | Shown when |
| --- | --- |
| Session | A process inside a live Session listens on a port. |
| Persistent | A registered user `systemd` unit is running and listening. It has no terminal. |

Tap a port for its details, **Open Web** and a public tunnel.

Mewla finds the listening ports itself: from `/proc` on Linux, so no `ss` or
other tool needs to be installed, and through `lsof` on macOS. It sees the
processes your user owns. Windows has no `lsof`, so Services lists no ports
there.

A service bound only to `127.0.0.1` is marked local; Mewla does not invent a
network URL for it. A registered unit that stopped shows **Inactive**; one
whose state cannot be read shows **Error** with the reason.

### Keep a service after its Session ends

A service started outside a Session, such as a user `systemd` unit, stays hidden
until you register it. Registering never starts, stops or changes the unit:

```sh
mewla service register -unit my-preview.service -name "Docs preview" \
  -project docs -port 3080 -cwd ~/projects/docs
mewla service list --json
mewla service unregister -unit my-preview.service   # removes the registration only
```

Only plain user unit names are accepted. Registrations survive daemon restarts
and Worker cleanup, and Mewla checks on every refresh that the unit is running
and owns the port. This needs Linux with a user `systemd`.

### Share one with a temporary public URL

A service can start a Cloudflare **Quick Tunnel**: a random
`trycloudflare.com` URL for that one service. It needs `cloudflared` on the
computer, but no Cloudflare account.

- **Anyone with the URL can reach the service.** Do not tunnel anything that
  holds secrets or has no login of its own.
- Mewla starts a tunnel only when you tap it, and first checks that the
  service answers HTTP.
- The tunnel follows the process that owns the port: if it exits, the tunnel
  stops. Stopping a tunnel never stops the service.
- URLs live in memory only. After a daemon restart there is no tunnel until you
  start one again.
- Cloudflare limits Quick Tunnels to 200 concurrent requests and does not
  support Server-Sent Events.

Mewla runs `cloudflared` with an empty configuration and never touches your own
named tunnels or `~/.cloudflared`. It learns the URL and when the tunnel is
connected from `cloudflared`'s own metrics endpoint on a loopback port, not by
reading its output. Quick Tunnels need a Linux daemon.

From the computer, with the ID and generation from `mewla service list --json`:

```sh
mewla service tunnel start  -id SERVICE_ID -generation PROCESS_GENERATION
mewla service tunnel status -id SERVICE_ID -generation PROCESS_GENERATION
mewla service tunnel stop   -id SERVICE_ID -generation PROCESS_GENERATION
```
