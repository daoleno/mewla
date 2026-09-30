# Terminal scrolling

Android and iOS use the same terminal path: PTY output updates the live
libghostty-vt grid. The embedded WebView owns vertical scrolling, including
finger tracking, momentum, and each platform's edge feedback. A scroll frame
does not send a terminal input, scroll request, or tmux command.

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

The live renderer continues to handle full-screen apps normally. While tmux
reports an alternate screen, history is disabled with an inline notice rather
than inventing a transcript of screen repaints. Input, cursor, and tap-to-focus
remain available. Leaving the alternate screen reloads normal pane history.
Inline TUIs can use pane history as usual.

On width changes, tmux remains responsible for terminal reflow. Zen fetches the
pane history again at its new width and retains the nearest physical reading
position; it does not promise that the same logical paragraph keeps its exact
pixel position after reflow. Switching pane, session, or current server replaces
or clears the old history. Failed history loads expose a retry notice without
replacing the live terminal.

Each snapshot is limited to 6,000 physical rows and 4 MiB of ANSI. Decoded HTML is
limited to 16 MiB of UTF-16 string storage. Only the viewport and two screens on either side are mounted
in the DOM. Native selection temporarily pins those nodes. During a touch, the
row under the finger is retained even when it leaves that mounted range. Live
HTML replacement waits for interaction to settle: replacing a touched DOM node
was the Android regression that dropped subsequent touch events after the first
few tmux scroll batches.

## Verification

Run the terminal history and native-scroll tests in `app/components/terminal`,
the app TypeScript check, and `go test ./...` for daemon changes. Runtime checks
need a real tmux pane with more than 6,000 mixed ANSI/CJK rows, an inline TUI and
an alternate-screen TUI, and Android/iOS device coverage. Record gesture/frame
timing and terminal history/copy-mode command counts separately from unrelated
daemon discovery work.

Never start a verification daemon against the user's home or state. Isolate
`HOME`, `CODEX_HOME`, tmux socket and temporary paths, and remove inherited
`ZEN_STATE_DIR` and Worker control variables. Record the real route-listener and
model-route state hashes before and after. A source checkout watched by
`zen-dev` cannot receive production Go edits without restarting that live daemon;
prepare such changes outside the watcher and obtain restart authorization before
integration.
