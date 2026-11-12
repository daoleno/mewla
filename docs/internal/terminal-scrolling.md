# Terminal scrolling

Android and iOS use the same terminal path: PTY output updates the live
libghostty-vt grid. The embedded WebView owns vertical scrolling, including
finger tracking, momentum, and each platform's edge feedback. Plain-screen
history scrolling stays entirely local. Alternate-screen scrolling forwards
bounded mouse-wheel batches through the existing authenticated terminal input
path so the application scrolls its own transcript.

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

A request without `since` (an older App), and every tmux Session, still gets the
snapshot described below. The App turns both into the same instruction for the
WebView: void the history, drop `removed` rows from its front, append `rows`
(`terminalHistoryModel.ts`).

Rows only ever append at the end, which moves nothing the reader sees, so the
WebView takes them at once, under a finger or a selection, and refreshes 250 ms
after output. Voiding and dropping rows replace nodes under the finger and wait
for the touch to end; held instructions merge in order.

Several viewers can show one host Session (the App, `mewla attach` on a
computer), and the program draws for one size: the viewer a person last acted
on sets it (attaching, a resize, typing). The host ends every other viewer that
asked to redraw (`AttachParams.Redraw`) with `AttachResized` when the size
changes, and the viewer attaches again without a size for a full reset and a
replay at the new size: output drawn for another width would otherwise write
rows over rows on its grid, and stay so after the sizes match again. A viewer
never takes the size back on its own (after `AttachBehind` or `AttachResized`),
or two viewers of different sizes would take it from each other. A smaller
viewer that is not the one acted on shows the other size wrongly until its
person acts (a focus-out report is not acting); viewers from before `Redraw`
keep their stream. A host from before `Redraw` answers `Attached.Redraw` false:
the daemon's view and `mewla attach` then take their size back whenever they
attach again and on every resize, as before.

## History and live output (tmux Sessions)

The authenticated `terminal_history` request takes an open `session_id` and is
checked against that connection's terminal-session owner. It captures the active
pane of that attachment's linked tmux view. It never enters tmux copy-mode.
The response includes pane identity, pane width, history size, alternate-screen
state, and ANSI history, excluding the live screen.

Captures preserve physical rows and ANSI attributes (`capture-pane -e -N`). A
separate 64-row Ghostty instance decodes them in small pages using the same theme
and cell HTML formatter as the live terminal. History is not converted to plain
text or reconstructed by comparing live screenshots. The live cursor stays in
the live grid. History selection/copy uses physical row breaks: tmux's ANSI
capture does not carry the live grid's soft-wrap metadata.

History loads on attachment and refreshes after output, resize, or pane focus.
Refresh is coalesced to a 750 ms idle timer, with one request/decoder at a time.
Touch, momentum, and native text selection suspend new requests and cancel stale
decoding; late results cannot replace a different attachment's history. The
WebView also defers incoming history replacements until interaction ends.

While reading older output, new output continues to update Ghostty. On the next
idle refresh, the viewport remains on the same retained row, including when the
bounded history drops old rows. If the reader's row has been evicted, the viewport
stops at the oldest retained row. **Scroll to bottom**, keyboard input, and the
keyboard accessory return to live output. Scrolling does not dismiss the IME or
resize the PTY. Taps on history do not send clicks to the live TUI; taps in the
live grid retain pane focus and terminal mouse/input behavior.

## Full-screen apps, resize, and limits

Pane metadata includes each pane's rectangle, alternate-screen state and mouse
reporting flag. Mewla chooses the pane under the touch, independently of tmux's
outer terminal modes. Plain-screen panes retain native history scrolling;
alternate-screen panes with mouse reporting use an invisible native scroll
range to turn browser scroll deltas (including momentum) into wheel ticks at
the touch cell. There is no alternate-screen error notice or copy-mode entry.

The same WebView implementation runs on Android and iOS. Native scrolling owns
fling, reversal and stop-on-touch. Batches are limited to three wheel ticks at
most once per animation frame (at least 16 ms apart). A batch waits for an
arriving app render before another is sent; intervening ticks are discarded, never queued. Because applications can
consume a reverse tick without redrawing, a 32 ms no-output deadline permits
a fresh tick; it never replays blocked deltas. Reversal and a new touch
release that wait so reaching a transcript boundary cannot trap the gesture.
Smoothness depends on app redraw and network latency. Output after an idle
interval publishes immediately, with sustained updates bounded to 16 ms and
presentation on the WebView animation frame. Native UI-queue dispatch avoids
an additional Fabric mounting-frame wait. The first vertical touch-slop crossing
primes one tick before the native scroll callback; native scrolling continues to
own distance and momentum. Each reversal emits a tick without waiting a full cell.

To soften line stepping, consecutive styled frames are compared for an exact
contiguous row translation with at least eight matching rows and four nonblank
rows. Only the longest proven region is interpolated; fixed headers, prompts
and badges retain their incoming rendering. ANSI spans and CJK cells are copied
unchanged. Interpolation finishes within one 16 ms display interval, without
carrying residual displacement into another frame. Using the previous PTY
redraw interval added up to 80 ms of old-content lag after output had already
arrived. Touch, reversal, selection and context changes cancel
it. Unmatched layouts and the system reduced-motion setting use direct redraws.
Touch targets remain in the DOM while a separate, non-interactive layer displays the updated transcript.
Touch cancellation retires that layer on the next frame, even without further
PTY output; it does not wait for the idle timer or cursor blink.
Selection suspends forwarding. Wheel input does not focus the IME, clear the
composer, resize the PTY, or synthesize a click.

This interpolation does not predict transcript rows that have not arrived.
Initial response still includes the browser's gesture recognition, the WebView
bridge and PTY round trip. Stopping prevents new wheel input immediately in the
handler, but an already returning app redraw can still arrive afterward. Native
momentum duration and post-momentum input backlog are different measurements;
a sustained fling is not expected to settle within 100 ms of finger release.

The native Ghostty mouse encoder uses the attached terminal's requested mode.
For tmux attachments, tmux receives that client encoding through its existing
PTY and translates it to the pane application's SGR 1006 or legacy X10 encoding
and pane-local coordinates. No subprocess is started per wheel tick. Pane
metadata/history refresh remains a coalesced idle operation, suspended throughout
the drag and fling. App start/exit and pane focus refresh this metadata after
output settles. Tmux does not expose DEC 1007 alternate-scroll opt-in; without
mouse reporting, Mewla leaves the gesture alone rather than injecting arrow keys
that could edit a prompt. A host Session does report it: on the alternate screen
with no mouse reporting and DEC 1007 on, as a desktop terminal treats it, the
swipe becomes cursor keys (`ESC [ A/B`, or `ESC O A/B` when the program asked
for application cursor keys), at most three per frame; with 1007 off or unknown
the gesture is left alone. Inline TUIs retain plain-screen history scrolling.

On width changes, tmux remains responsible for terminal reflow. Mewla fetches the
pane history again at its new width and retains the nearest physical reading
position; it does not promise that the same logical paragraph keeps its exact
pixel position after reflow. Switching pane, session, or current server replaces
or clears the old history. Failed history loads expose a retry notice without
replacing the live terminal.

Each snapshot is limited to 6,000 physical rows and 4 MiB of ANSI. Decoded HTML is
limited to 16 MiB of UTF-16 string storage. Only the viewport and two screens
on either side are mounted
in the DOM. Native selection temporarily pins those nodes. During a touch, the
row under the finger is retained even when it leaves that mounted range. Live
HTML replacement waits for interaction to settle: replacing a touched DOM node
was the Android regression that dropped subsequent touch events after the first
few tmux scroll batches.

## Selecting, links, prompts, search

Rows carry their wrap flags as `data-wrap` and `data-wrap-continuation`, so
copying a selection joins the rows a soft wrap split, in history as in the live
grid. While a selection is held, 600 rows on either side of the viewport are
mounted and none are unmounted, so a selection can be dragged across the
scrollback.

A tap on a URL (http, https) opens it; one on a path (absolute, `~/`, `./`,
`../`, or with a directory and an extension, with an optional `:line:column`)
or a `file:` URL opens the Session file preview, for a Worker whose run is
known. The line is the logical one: a link a soft wrap split is found whole.
A program that reports the mouse keeps taps on its own screen; history is
always static. `terminalLinks.ts` holds the matcher.

While scrolled up, the buttons beside **Scroll to bottom** jump to the previous
and next prompt and open search. Prompts are the rows the program marked with
OSC 133; when no row of the history is marked, lines that look like a prompt
(`❯ `, `› `, `$ `, `user@host … $`) stand in. Next past the last prompt returns
to the live screen. Search is over the history rows' text, case-insensitive;
the match shows as a tinted row, older and newer matches are the arrows.

## Verification

Run the terminal history and native-scroll tests in `app/components/terminal`,
the wheel encoding/selection tests, the app TypeScript check, and `go test ./...`
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
