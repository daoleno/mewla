# Terminal rendering

The terminal view (`client/ui/terminal/contract.ts`) has one contract and three
renderers. Android (Kotlin: Canvas text and an `InputConnection`,
`client/android/app/src/main/java/com/daoleno/mewla/terminal`) and iOS (Swift:
CoreText and `UITextInput`, `client/ios/Sources/Terminal`) are native components
that hold libghostty-vt; the web draws it in a web view page running xterm.js
(`client/ui/terminal/web`, `client/ui/web/terminal`). PTY output and input use the canonical server's
authenticated WebSocket connection; the view has no connection or credentials of
its own.

The VT lives in the view. The page passes the computer's output in (`write`,
`reset` for a new attachment) and sends what the view reports to the computer:
`input` (committed text, keys, pasted text), `size`, `scroll`, `tapCell`. IME
composition happens inside the view and only committed text becomes `input`.
Selection and copy are the platform's own (long press, system menu). Text a
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
through a second VT and scrolls through them itself. Search and the prompt jumps
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
