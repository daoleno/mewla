# Terminal scrolling

Android and iOS use the same path: PTY output updates the live libghostty-vt
grid in the terminal view, and the history above it is the rows the page hands
the view. The view scrolls through those rows itself, with the platform's own
finger tracking and momentum. While the program is on the alternate screen, a
drag goes to the program: as wheel presses when it takes mouse reports, as
cursor keys when it asks for them (DEC 1007), and not at all otherwise
(`client/core/terminal/wheel.ts`). A tap on a history row sends nothing to the
live program.

## Numbered scrollback

The session host's libghostty-vt keeps each Session's history (10,000 rows),
and the App is sent only what it is missing. `terminal_history` takes a
`since` cursor, `{epoch, next}` (a request without one is refused), and the
answer is a delta:

- `lines` are styled ANSI rows, oldest first, and `flags` has one bit set per
  row: 1 soft-wrapped onto the next row, 2 continues the row above, 4 starts a
  shell prompt (OSC 133), 8 continues a prompt.
- Rows are numbered from the first the daemon saw. `first` is the oldest row
  still kept, `from` the number of `lines[0]`, `next` the number after the last
  row. The App's next request names `{epoch, next}`.
- The daemon numbers a view's rows by matching what each capture shares with
  the last one (`daemon/terminal/scrollback.go`); no host support is needed,
  so a host that predates marks and modes still works. A new `epoch` with
  `reset: true` voids what the App holds: a resize (every row reflows), the
  scrollback erased, or more new output than the history keeps. A cursor from
  another epoch, another view or another daemon run also gets a reset.
- While the program is on the alternate screen the answer carries no rows and
  the viewer keeps its place; the primary history is unchanged until it leaves.
  A program that starts on the alternate screen (Claude Code) leaves nothing
  numbered yet, so the answer to a viewer that holds nothing is empty at epoch
  0, not a reset. The App takes that as in step. Before it did, the App voided
  its view and asked again every 750 ms, which dropped a long-press selection
  half a second after it appeared.
- `alt_scroll` (DEC 1007) and `app_cursor` (DECCKM) come with every answer
  from a host that reports them, `mouse` always. Absent means unknown.

Several viewers can show one host Session (the App, `mewla attach` on a
computer), and the program draws for one size: the viewer a person last acted
on sets it (attaching, a resize, typing, the App's terminal shown again). The
host ends every other viewer that asked to redraw (`AttachParams.Redraw`) with
`AttachResized` when the size changes, and the viewer attaches again without a
size for a full reset and a replay at the new size: output drawn for another
width would otherwise write rows over rows on its grid, and stay so after the
sizes match again. A viewer never takes the size back on its own (after
`AttachBehind` or `AttachResized`), or two viewers of different sizes would take
it from each other. A viewer that is not the one acted on still shows the
screen right until its person acts (a focus-out report is not acting): the
daemon tells the App's view the Session's size (`terminal_size`) before each
replay, and the view's VT takes that grid and draws it fitted into its area
(scaled down to 6 dp text, then panned; `TerminalFit`), while `mewla attach`
mirrors the host's screen cut to its window (`mirrorFrame`) instead of writing
output meant for another size. Viewers from before `Redraw` keep their stream.
A host from before `Redraw` (every Session started before the upgrade runs on
one until the host restarts)
answers `Attached.Redraw` false and never ends a viewer for a resize: the
daemon's view and `mewla attach` then ask the Session's size (`Info`) every
half second and start over the same way when it is not the size their replay
was drawn at.

The App turns every answer into one instruction for the view: void the
history, drop `removed` rows from its front, append `rows`
(`client/core/terminal/history.ts`). History loads on attachment and refreshes
250 ms after output, one request at a time; a late answer cannot replace a
different attachment's history. While scrolled up, the rows on screen stay put
and new output keeps updating the live grid. If the reader's row has been
evicted, the viewport stops at the oldest retained row. **Scroll to bottom**,
keyboard input, and the keyboard accessory return to live output. Scrolling
does not dismiss the IME or resize the PTY.

## Full-screen apps, resize, and limits

On width changes the session host reflows the terminal and starts a new epoch.
The App fetches the history again at its new width and retains the nearest
physical reading position; it does not promise that the same logical paragraph keeps its exact
pixel position after reflow. Switching Session or current server replaces
or clears the old history. Failed history loads expose a retry notice
(`retryHistory`) without replacing the live terminal.

Each history delta is limited to 6,000 physical rows
(`HISTORY_MAX_ROWS`) and 4 MiB of ANSI (`HISTORY_MAX_BYTES`).

## Selecting, links, prompts, search

Rows carry their wrap flags (`HISTORY_ROW_WRAPPED`, `HISTORY_ROW_CONTINUATION`),
so copying a selection joins the rows a soft wrap split.

A tap on a URL (http, https) opens it; one on a path (absolute, `~/`, `./`,
`../`, or with a directory and an extension, with an optional `:line:column`)
or a `file:` URL opens the Session file preview, for a Worker whose run is
known. The tap reports the row's text and the tapped cell's index, so the link
is found in the logical line. `linkAction` in `client/core/terminal/text.ts`
holds the matcher.

While scrolled up, the buttons beside **Scroll to bottom** jump to the previous
and next prompt and open search. Prompts are the rows the program marked with
OSC 133; when no row of the history is marked, lines that look like a prompt
(`❯ `, `› `, `$ `, `user@host … $`) stand in. Next past the last prompt returns
to the live screen. Search is over the history rows' text, case-insensitive;
the match shows as a tinted row, older and newer matches are the arrows.

## Verification

Run the terminal tests in `client/core/terminal` (`cd client && bun test core/terminal`) and
`client/ui/terminal` (`cd client/ui && nsh test terminal`), `nsh check`, and `go test ./...`
for daemon changes. Runtime checks
need a real Session with more than 6,000 mixed ANSI/CJK rows, an inline TUI and
an alternate-screen TUI, and Android/iOS device coverage. Record gesture/frame
timing and terminal history request counts separately from unrelated
daemon discovery work.

Never start a verification daemon against the user's home or state. Isolate
`HOME`, `CODEX_HOME`, the state directory and temporary paths, and remove inherited
`MEWLA_STATE_DIR` and Worker control variables. Record the real route-listener and
model-route state hashes before and after. A source checkout watched by
`mewla-dev` cannot receive production Go edits without restarting that live daemon;
obtain restart authorization before editing, then verify Brain host binding,
replacement log, route listeners and Worker observation after each restart.
