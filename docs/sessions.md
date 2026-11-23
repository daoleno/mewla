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
| Claude, Codex, Cursor, Grok, Pi, OpenCode, Antigravity | That agent CLI, using the command from [Agents and models](executors.md) |
| Shell | Your login shell |
| DSH | The DSH harness, opened as a web page rather than a terminal |

**Advanced** lets you set any command (for example `claude --model opus`) and a
window title. The Session starts on the current computer.

The agent must already be installed and signed in on the computer; Mewla never
signs in for you. The sheet dims the agents this computer does not have.
Starting one is refused before any Session exists, and the sheet says so in one
sentence ("Antigravity is not installed on workbench."). The daemon's
`create_session` answers `agent_not_installed` with the agent's name, its
program and, where Mewla has one, the install command as separate fields (the
CLI and a later Install action use them; the app does not print them). A shell and a custom
command are never refused for a program that is not an agent's.

For an agent, **Task** ("What should it do?") is the Session's first message.
The computer holds it until the agent is ready; meanwhile it shows in the
Session as sending, and anything else you send waits behind it and goes after
it, in order. If it could not be delivered (or the connection dropped while it
was held), it leaves the chat and comes back as the Session's draft. Mewla
never sends it again by itself. A message sent from Chat replaces whatever is
typed but not sent in the agent's input in the terminal, every line of it.

Codex starts inline (`--no-alt-screen`): its transcript goes into the
terminal's scrollback, so the phone's Terminal and a computer's own terminal
scroll it on their own. If your Codex config names `[tui] alternate_screen`, or
the command chooses the screen itself, Mewla leaves it as you set it. Claude
Code keeps the renderer it picks for itself, or the `tui` your Claude settings
name.

## Chat and Terminal

A Session's header has a **Chat / Terminal** switch. The choice is remembered
per Session. What you type in either goes to the same terminal. When the
Session's folder is a Git repository, a **Git** button sits beside the switch;
it opens [Git review](#git-review).

- **Chat**: the structured conversation, with your messages, the agent's
  replies, tool calls, plans and pictures. A run of finished tool calls folds
  into one row that says what it did ("Read 3 files"); tap it for the calls. A
  call that failed is folded like any other and the row says so; a call that is
  running or waiting on you stays on its own row, and so does a subagent, which
  reads as the task it was given. On Claude, tap a subagent to see the calls it
  made, folded the same way, with how many there were, how long it took and
  whether it is still working. Chat is available for Claude, Codex, Cursor,
  Grok, Pi and OpenCode. A delegated Worker's Chat opens with the task Brain
  gave it, two lines at most; tap it to read the whole task.
- **Terminal**: the live terminal, for any command, a Shell included. The key
  bar adds Esc, Tab, Ctrl (armed for one key), ⌃C, ⌃D, ⌃B and arrows that
  repeat while held. Scroll up for **Search history**, **Previous prompt**,
  **Next prompt** and **Scroll to bottom**. Tap a link to open it in the
  browser, or a file path to preview the file.
  On Android, pinch to change the text size between 8 and 14 (10 by default).
  The phone remembers it, and the terminal takes the new size once you lift
  your fingers. Drag one finger up or down to scroll back through what the
  Session printed, and flick to keep going; it returns to the live screen at
  the bottom. A program that fills the whole screen (Claude Code, Codex, `less`)
  has no history to scroll, so the drag goes to it the way a mouse wheel does:
  as wheel presses when it reads the mouse, or as Up and Down when it lets the
  wheel send arrow keys; it scrolls its own text as far as you drag (Claude
  Code speeds up wheel presses that come close together, so it gets fewer of
  them), and stops when you touch the screen. In a program that reads the
  mouse, a tap is a click there (Claude Code's "Jump to bottom"); the Keyboard
  key brings up the keyboard.

When a Session is blocked on a prompt the chat cannot answer, it opens on the
Terminal by itself.

### Copy and paste in the Terminal on a phone

Press and hold a word to select it. Drag either handle to stretch the selection
over any rows, including the history above the screen, which scrolls as you
reach an edge. The menu offers **Copy**, **Paste** and **Select All**; holding
a blank spot offers **Paste** and **Select All**. A row that wrapped onto the next copies as
one line. Paste goes to the program as typed text, framed as a paste when the
program asks for that (bracketed paste), so a shell or an agent never runs a
pasted line before you press Return. On a hardware keyboard, Ctrl+Shift+C and
Ctrl+Shift+V copy and paste on Android.

Alt from the keyboard bar, or Alt held with a letter that types itself, reaches
the program as Alt (an escape before the key) on iPhone and iPad as on Android,
so Alt+b and Alt+f move by words in a shell. A character that Option types on
an iPad or Mac keyboard (Option+l is @ on a German layout) is typed as that
character.

### Copy from the Terminal in a browser

Select text with the mouse and press **Cmd+C** on a Mac, or **Ctrl+C** or
**Ctrl+Shift+C** on Windows and Linux; the text goes to your computer's
clipboard. With nothing selected, Ctrl+C still interrupts the program (on a
Mac it always does), and right after a Ctrl+C copy the next Ctrl+C interrupts
again. A program that uses the mouse, such as `vim` or an agent's interface,
keeps plain drags for itself: hold **Shift** (**Option** on a Mac) while you
drag to select instead. What a program copies with the terminal's clipboard
code (OSC 52) reaches the browser's clipboard too, on `localhost` or an HTTPS
address. Mewla never lets a program read your clipboard.

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

The line an answered card leaves at the bottom of a chat is only there until
the chat moves on: once anything newer arrives, the answer is history (the
Session's own chat keeps it where it was asked). A Worker's request that Brain
answered leaves no line in Brain's chat, since Brain's own step already says
so. Nothing there needs closing.

### Copy and edit a message

Tap one of your messages for **Copy**, **Select Text** and **Edit**. Code blocks
have their own **Copy**. Copy gives the text as written, Markdown included.

Messages are read as standard GitHub-flavored Markdown on every platform:
headings, quotes, nested and numbered lists, task lists, tables,
strikethrough, links and bare URLs, HTML entities such as `&amp;`, images,
and code blocks with a copy button. A reply that is still arriving never shows
half-typed syntax: an unfinished `-`, `1.` or `>` waits for its text.

A code block tagged `mermaid` (flowcharts, sequence, state, gantt, class, ER,
pie, mindmap, timeline and the other Mermaid diagram types) shows as its
diagram, in light or dark with the app, here and in a Markdown file you
preview. It is laid out for the width it has: a wide left-to-right flowchart
turns top-to-bottom on a phone, and one that cannot get narrower scrolls
sideways at a size you can read rather than shrinking. A tall one shows its
top. Tap it, or its expand button, for the diagram full screen as written:
fitted first, then **+** and **−** zoom it and you scroll to move around. Its
**Copy** copies the source. While the reply is still arriving the block shows
its code, and it is drawn once, when the reply ends. A diagram that cannot be
read stays code, with the parser's error under it. Diagrams are drawn by
[Pele](https://pele.run) in the app itself, with no network. Its layout and
look differ from Mermaid's own; HTML in labels other than line breaks, bold
and italic shows as text.

**Edit** puts the message back in the composer; with a hardware keyboard (a
phone's too), ↑ in the empty composer edits your last message and Esc leaves
the edit. What sending does depends on the agent:

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
without leaving the chat: Markdown rendered, text and code as text, HTML as a
page (in a view of its own that reaches nothing else of the app's), images you
can zoom, PDFs page by page (pinch or double tap to zoom on the phone; in a
browser the PDF opens in a tab of its own), video (mp4, m4v, mov, webm) and
audio (mp3, m4a, wav, ogg, opus, flac). A full path to a file in your home
folder, such as `~/notes/plan.md`, is a link wherever a reply names it, in
code or in the text; a path in a code block is not. Any other path a reply
names, such as `worklog/plan-1116/DESIGN.md`, `./out/report.pdf`,
`src/main.go:42` or `/tmp/shot.png`, becomes a link once the computer says it
is a file there: a relative path is read from the folder the agent works in
(Brain's: its workspace). A path that names no file stays text. A computer
on an older Mewla links only home paths until it is updated.
Playback streams from the computer, so a long recording starts at once and
seeking does not download the whole file. Every file has **Download**; text
files (Markdown, code, txt, JSON) also have **Copy**, which copies the whole
file.

Web addresses, with or without `[text](…)`, open in your browser, including one
that sits right beside Chinese text or punctuation, and email addresses open
your mail app. A link that names a file, such as `[report](report.pdf)` or
`[app](./out/app.apk)`, opens that file as above. Inside a Markdown file you
opened, a link to another file is relative to that file. Links that can run
code (`javascript:`, `data:`) never open. A changed-files list shows each file
with the lines it added and removed (+12 −3); a renamed file shows, and opens,
its new path.

A file that is not really what its name says (an `.mp4` that is not an MP4
container, say) is not played. A codec the device can't decode, such as HEVC
in some browsers, can't be played there; download it instead.

### Pictures in a chat

Pictures you sent show as pictures above your message (other files stay a row
in it). A picture a tool call produced, such as an agent's screenshot, is a
thumbnail under its row. Tap any picture to open the viewer: black, with × and
"2 of 3" on top. Previous and next sit at the sides, and the arrow keys and a
swipe step through every picture of the chat in order; double-tap or pinch to
zoom, and Back closes it. While a picture loads, its frame is a plain block
(a thin moving line along its top once it takes more than 3 seconds). A load the
connection cut waits for the connection and loads by itself; with the
connection up, an unanswered load tries again after 1, 2, 4 and 8 seconds. Only
then, or when the computer says the file is no picture it can show, does the
frame show a refresh mark; tap it to load it again.

Every picture can be kept. The viewer's top bar has Save and Share on the
phone (and Copy on iOS); a long press on a picture in the chat offers the same.
Save puts the original file, at full size and in its own format, in the phone's
gallery: Pictures/Mewla on Android (Android 9 asks for storage the first time),
Photos on iOS (which asks once for permission to add). Share opens the system
share sheet with that file. On the web the same buttons are Download and Copy.
A picture in a Calendar run's result opens the same viewer. If saving fails, a
line at the bottom says why.

Paste a screenshot or a copied picture into the composer and it becomes a chip
above the field, the same as one you attached; pasted text goes in as typed. On
Mewla for Mac a file copied in Finder pastes as that file (not its name). A
spreadsheet cell or a selection from a web page pastes as its text only, without
the picture of itself those apps copy along. The phone's keyboard can paste a
picture the same way.

On Mewla for Mac and in a browser, drag files from Finder, the desktop or another
app onto the conversation or the composer: "Drop to attach" shows while they are
over it, and dropped they become chips, the same as attached ones (in Brain's
chat, a Session's chat and a Work's reply). On the Mac an image dragged out of a
web page (Safari, Chrome) and the screenshot thumbnail that floats in the corner
after ⌘⇧3 or ⌘⇧4 drop the same way. A folder is not attached. One drop or paste
takes up to 512 MB in all, as **+** does; a file past that is a chip that says
so, and a file macOS would not let Mewla read is a chip that says it can't be
read. Text dragged into the field goes in as text, as before.

In a terminal, **Attach** sends a file to the computer and types its path at the
prompt, quoted for the shell. On the phone it is the first key of the key bar;
on Mewla for Mac it is the paperclip, first in the header's actions while the
page shows the terminal. While a file goes up its chip shows the progress and
Cancel. A file that fails stays as a chip with the reason, Retry and ×.

## The Session's menu

**⋯** in a Session's header holds:

| Item | Does |
| --- | --- |
| **Files** | The Session's [files](#files) |
| **Model & Effort** | The model and effort for this Session only, from the agent's own list ([A Session's models](executors.md#a-sessions-models)); while you write, the model also shows beside **Send** |
| **Open Web** | A DSH Session's web page |
| **New Terminal** | A Shell in the same folder and on the same computer |
| **Rename** | The Session's name |
| **Open its Work** | Work detail for the Work this Worker does |
| **Terminate** | Stops the Session, after you confirm |

## Git review

**Git** in the Session's header, or the changed-files card in the chat, opens
the Session's repository:

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
a Session to the grid.

On Linux with a systemd user session, the session host runs as a user service
of its own (`mewla-session-host-…`), not inside the terminal or service that
started Mewla. When the computer runs out of memory and the kernel ends one
process, systemd does not stop the host and the Sessions with it; closing the
terminal Mewla was started from does not either. Without a systemd user
session the host starts directly.

## How Mewla follows each agent

Where an agent offers a way to be followed, Mewla uses it instead of reading
the screen or the agent's files:

| Agent | Followed through | Messages and Stop |
| --- | --- | --- |
| Claude Code | Its hooks, set when Mewla starts it | Typed into the Terminal; the hook confirms delivery |
| Codex | An app-server beside the Terminal, set when Mewla starts it (Codex in the Terminal starts once the app-server is listening); the conversation is read from the thread Codex announced, and the Status sheet asks the same app-server (account, usage limits, model, approvals, sandbox, token use) | Sent to Codex directly |
| OpenCode | OpenCode's background service: one event stream for all Workers, the session id chosen at launch | Sent to the service directly; a message to a session OpenCode has not created yet (its first) is typed |
| Pi | A small extension Mewla loads into each Pi it starts (`pi -e`), which reports Pi's turns over a local socket; the session file is chosen at launch | Handed to Pi by the extension; Stop aborts the running turn |
| Cursor | The chat is created before Cursor starts and resumed, so its transcript is known from the first moment; turns are read from the transcript and the screen | Typed into the Terminal |
| Grok, dsh | The agent's transcript and the screen | Typed into the Terminal |
| Antigravity | `agy acp`, held by the Session's own program (`mewla agy-session`), which reports agy's turns, tool calls, approvals and questions over a local socket and keeps the conversation's log; an agy started by hand and adopted is replayed by `agy acp` when its file changes | Sent to agy directly; Stop cancels the turn (typed into the Terminal for an adopted agy) |

Stop means the work stops. It ends the running turn, and also the commands
that turn started and left running in the agent's background terminal: a
Codex command still running after its tool call returned is ended through
Codex's app-server, and a Claude Code Bash command (run in the background or
not) is ended by its process group. Commands an earlier turn started keep
running. The chat says what was ended in one line under the turn, such as
"Stopped 1 command this turn started: seq 1 30", or "1 command could not be
stopped: …" when one could not be. If a Claude Code update changes how it
runs Bash commands so that Mewla recognizes none of the turn's, Stop leaves
them alone, the line says "N commands may still be running", and the daemon
log names the Claude Code version. OpenCode and Pi end their running command
when they stop, and have no background commands. The line is not kept when
the daemon restarts.

A Worker that Mewla did not start, or that started with its own settings, is
followed from the transcript and the screen, and the app offers only what that
supports. For Codex that is the one case still found by looking: a Codex you
opened by hand, like the Brain when it runs on Codex, has no app-server of
Mewla's, so its conversation is matched by folder and start time in Codex's
own thread records (read by Mewla itself, on Windows too) and by the files it
holds open, and its Status sheet says to type `/status` in the Terminal. A Codex that Mewla started is not looked for
at all: until it announces its thread (a moment after it starts) there is
nothing to read, and only if it has not announced one after 30 seconds is its
transcript searched for.

Folder trust follows the same rule. Codex is trusted for a Worker's folder by
the Worker's own command line, so Mewla leaves your Codex config alone. Claude Code
has no per-launch way to do that, so for Claude Workers Mewla sets that one
folder's `hasTrustDialogAccepted` in Claude's own config, which is the key
Claude itself names for it. A Claude Brain host gets the same for the Brain
workspace, so a first launch on a new machine does not wait at the dialog. A
Claude Session you create in a folder you pick gets it for that folder: picking
the folder in Mewla is your answer, and the dialog would otherwise wait where
Chat cannot show it (it opens on "No, exit"). Your home folder and a drive root
are never trusted this way. On Windows the key goes into the `.claude.json` in
your user folder (`%USERPROFILE%`), where Claude reads it.

Cursor and Grok can only be hooked through the user's own global
configuration, and Mewla never writes to it. They stay on the transcript and the
screen, and so does anything an agent offers only through such a file.

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
