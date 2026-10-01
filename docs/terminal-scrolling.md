# Terminal scrolling

Android and iOS use the same terminal path: PTY output updates the live
libghostty-vt grid. The embedded WebView owns vertical scrolling, including
finger tracking, momentum, and each platform's edge feedback. Plain-screen
history scrolling stays entirely local. Alternate-screen scrolling forwards
bounded mouse-wheel batches through the existing authenticated terminal input
path so the application scrolls its own transcript.

## History and live output

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
reporting flag. Zen chooses the pane under the touch, independently of tmux's
outer terminal modes. Plain-screen panes retain native history scrolling;
alternate-screen panes with mouse reporting use an invisible native scroll
range to turn browser scroll deltas (including momentum) into wheel ticks at
the touch cell. There is no alternate-screen error notice or copy-mode entry.

The same WebView implementation runs on Android and iOS. Native scrolling owns
fling, reversal and stop-on-touch. Batches are limited to three wheel ticks at
most once per animation frame (at least 16 ms apart). A batch waits for an
arriving app render before another is sent; intervening ticks are discarded, never queued. Because applications can
consume a reverse tick without redrawing, a 100 ms no-output deadline permits
a fresh tick; it never replays blocked deltas. Reversal and a new touch
release that wait so reaching a transcript boundary cannot trap the gesture.
Smoothness depends on app redraw and network latency. Wheel-response output is
coalesced in one JS task, with presentation on the WebView animation frame,
avoiding an additional React Native animation-frame wait. The first recognized
native scroll delta and each reversal emit a tick without waiting a full cell.

To soften line stepping, consecutive styled frames are compared for an exact
contiguous row translation with at least eight matching rows and four nonblank
rows. Only the longest proven region is interpolated; fixed headers, prompts
and badges retain their incoming rendering. ANSI spans and CJK cells are copied
unchanged. Interpolation uses the previous redraw interval, capped at 80 ms,
with no animation queue. Touch, reversal, selection and context changes cancel
it. Unmatched layouts and the system reduced-motion setting use direct redraws.
Touch targets remain in the DOM while a separate, non-interactive layer displays the updated transcript.
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
mouse reporting, Zen leaves the gesture alone rather than injecting arrow keys
that could edit a prompt. Inline TUIs retain plain-screen history scrolling.

On width changes, tmux remains responsible for terminal reflow. Zen fetches the
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
`ZEN_STATE_DIR` and Worker control variables. Record the real route-listener and
model-route state hashes before and after. A source checkout watched by
`zen-dev` cannot receive production Go edits without restarting that live daemon;
obtain restart authorization before editing, then verify Brain host binding,
replacement log, route listeners and Worker observation after each restart.
