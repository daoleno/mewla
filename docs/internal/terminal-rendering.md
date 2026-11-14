# Terminal rendering

The terminal view (`client/ui/terminal/contract.ts`) has one contract and three
renderers. Android (Kotlin: Canvas text and an `InputConnection`,
`client/android/app/src/main/java/com/daoleno/mewla/terminal`) and iOS (Swift:
CoreText and `UITextInput`, `client/ios/Sources/Terminal`) are native components
that hold libghostty-vt. The web runs the same libghostty-vt, with the phones'
packer and glyphs (`client/native/terminal`), as WebAssembly in a web view page
that draws on a canvas (`client/ui/terminal/web`, `client/ui/web/terminal`;
`scripts/build-libghostty-web.sh` builds `mewla-vt.wasm` from the release pinned in
`native/terminal-vt/native.lock.json`). The page is NSH's web view, a srcdoc frame
sandboxed to an opaque origin that inherits the app's Content-Security-Policy: its
script, faces and VT come from the app's `/terminal/`, read cross-origin (the daemon
marks those files `Access-Control-Allow-Origin: *`), and the policy names the app's
own origin beside `'self'`, because WebKit matches an inherited `'self'` against
the frame's opaque origin and refuses them otherwise. `scripts/terminal-corpus/web.ts`
renders the corpus through that page in Chromium, Firefox or WebKit (`ENGINE`) at a
real device scale. All three draw the packer's runs on a grid of
whole device pixels and draw box, block, braille and Powerline glyphs themselves, so
rows of lines and blocks meet. Each keeps a character's width per face, measured
once (whether it fills exactly one cell decides how a run is drawn), and sets the
face only when a run's differs: a desktop-size grid draws tens of thousands of
cells a frame. PTY output and input use the canonical server's
authenticated WebSocket connection; the view has no connection or credentials of
its own.

The VT lives in the view. The page passes the computer's output in (`write`,
`reset` for a new attachment) and sends what the view reports to the computer:
`input` (committed text, keys, pasted text), `size`, `scroll`, `tapCell`. IME
composition happens inside the view and only committed text becomes `input`
(on the web, a hidden textarea at the cursor takes the keyboard and the input
method, the composition is drawn at the cursor, and keys are encoded by
libghostty-vt for the program's modes). Selection and copy are the platform's own
(long press and the system menu on the phones; drag, double-click a word and
triple-click a line, then the browser's copy, on the web). Text a
program copies (OSC 52, OSC 1337 Copy, OSC 5522) arrives as `copy`. If the VT
cannot be made, the view reports `failed` and `retry` tries again.

The daemon sends the first PTY output after an idle interval immediately, instead
of charging every echo a fixed 16 ms batch delay. Bursts retain their trailing
timer and 8 KiB message bound. Incomplete UTF-8 tails are retained across PTY
reads so immediate publication cannot split a Unicode character in JSON.

The history above the live screen is not the VT's. The computer keeps it
(`terminal_history`, `client/core/terminal/history.ts`) and the page hands its
rows to the view (`history`: void with `reset`, drop `removed` rows from the
front, append `rows` as styled ANSI), which draws them above the live screen
through a second VT and scrolls through them itself. The view keeps the rows'
ANSI and draws a page of 64 rows the first time one of its rows is needed (on
screen, selected or copied), so a flood that hands over thousands of rows costs
only an append; a theme or width change voids the drawn pages. Each row
therefore has to stand on its own: session-host rows start and end their style
with a reset, and core's `standAloneRows` carries a tmux capture's style from
the rows above into each row. Search and the prompt jumps
run in the page over the same rows and move the view (`scrollTo`, `mark`). See
[terminal scrolling](terminal-scrolling.md) for that contract and its limits.

Focused behavioral tests live beside the page code (`client/core/terminal`,
`client/ui/terminal`, and on iOS the terminal unit tests `client.yml` runs).
Performance acceptance additionally requires device traces for input latency,
flood responsiveness, frame pacing and scrolling; unit tests do not certify those
budgets. iOS native compilation and device checks require an Apple build host.

Brain host and ordinary Worker terminal mode share `TerminalPane`
(`client/ui/terminal/TerminalPane.tsx`). Structured chat is a separate overlay,
removed in terminal mode.
