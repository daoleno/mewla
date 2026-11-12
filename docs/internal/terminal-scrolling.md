# Terminal scrolling

Android and iOS use the same path: PTY output updates the live libghostty-vt
grid in the terminal view, and the history above it is the rows the page hands
the view. The view scrolls through those rows itself, with the platform's own
finger tracking and momentum. With no history, or while the program is on the
alternate screen, a drag is not taken: the view reports it (`scroll`, in lines,
positive going back in time) and the page sends `terminal_scroll`, which moves
the computer's tmux copy mode. `terminal_scroll_cancel` leaves it. A tap on the
live screen focuses the pane under the finger (`terminal_focus_pane`); a tap on a
history row sends nothing to the live program.

## Session host Sessions: numbered scrollback

A Session that runs in the session host (`mewla host`) has no copy-mode and no
snapshot. The host's libghostty-vt keeps the history (10,000 rows), and the
App is sent only what it is missing. `terminal_history` takes an optional
`since` cursor, `{epoch, next}`, and a host Session answers with a delta:

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
it from each other. A smaller viewer that is not the one acted on shows the
other size wrongly until its person acts; viewers from before `Redraw` keep
their stream.

A request without `since` (an older App), and every tmux Session, still gets the
snapshot described below. The App turns both into the same instruction for the
view: void the history, drop `removed` rows from its front, append `rows`
(`client/core/terminal/history.ts`). While scrolled up, the rows on screen stay
put.

## History and live output (tmux Sessions)

The authenticated `terminal_history` request takes an open `session_id` and is
checked against that connection's terminal-session owner. It captures the active
pane of that attachment's linked tmux view. It never enters tmux copy-mode.
The response includes pane identity, pane width, history size, alternate-screen
state, and ANSI history, excluding the live screen.

Captures preserve physical rows and ANSI attributes (`capture-pane -e -N`). The
view renders them with a second VT in the live terminal's theme, a page of
rows at a time when first needed (see [terminal rendering](terminal-rendering.md)). History is not
converted to plain text or reconstructed by comparing live screenshots. The live
cursor stays in the live grid. History selection/copy uses physical row breaks:
tmux's ANSI capture does not carry the live grid's soft-wrap metadata.

History loads on attachment and refreshes after output, resize, or pane focus.
Refresh is coalesced to a 750 ms idle timer (`snapshotMs`), with one request at
a time; late results cannot replace a different attachment's history.

While reading older output, new output continues to update the live grid. On
the next idle refresh, the viewport remains on the same retained row, including
when the bounded history drops old rows. If the reader's row has been evicted,
the viewport stops at the oldest retained row. **Scroll to bottom**, keyboard
input, and the keyboard accessory return to live output. Scrolling does not
dismiss the IME or resize the PTY.

## Full-screen apps, resize, and limits

On width changes, tmux remains responsible for terminal reflow. Mewla fetches the
pane history again at its new width and retains the nearest physical reading
position; it does not promise that the same logical paragraph keeps its exact
pixel position after reflow. Switching pane, session, or current server replaces
or clears the old history. Failed history loads expose a retry notice
(`retryHistory`) without replacing the live terminal.

Each history snapshot or delta is limited to 6,000 physical rows
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
need a real tmux pane with more than 6,000 mixed ANSI/CJK rows, an inline TUI and
an alternate-screen TUI, and Android/iOS device coverage. Record gesture/frame
timing and terminal history/copy-mode command counts separately from unrelated
daemon discovery work.

Never start a verification daemon against the user's home or state. Isolate
`HOME`, `CODEX_HOME`, tmux socket and temporary paths, and remove inherited
`MEWLA_STATE_DIR` and Worker control variables. Record the real route-listener and
model-route state hashes before and after. A source checkout watched by
`mewla-dev` cannot receive production Go edits without restarting that live daemon;
obtain restart authorization before editing, then verify Brain host binding,
replacement log, route listeners and Worker observation after each restart.
