# Mobile terminal rendering

Android and iOS use libghostty-vt for terminal state and the same WebView row
renderer. PTY output and input retain the canonical server's authenticated
WebSocket connection. The WebView does not own a second connection or credentials.

The shared native formatter produces indexed row updates. Each update contains
the grid dimensions, cursor state, and matching `dirtyLines` / `lineHtml` arrays.
`lineHtml[i]` replaces row `dirtyLines[i]`. Cursor-only changes carry empty arrays.
The native cache holds at most one visible grid and compares formatted dirty rows
so an application's clear/redraw of unchanged cells does not resend those rows.
Cell styling, Unicode, wrap metadata, and row HTML use the same formatter as full
history pages.

A new model, resize, theme change, or WebView reload establishes a complete base.
The RN controller does not consume dirty native state before the WebView is ready,
so an unmounted renderer cannot lose the base needed by later partial updates.
Output after an idle interval publishes immediately. Sustained output retains
dirty state in Ghostty and has at most one trailing task, with 16 ms between
publications. The WebView accumulates every delta and paints the latest state
once per animation frame. There is no separate RN animation-frame wait.

The daemon also sends the first PTY output after an idle interval immediately,
instead of charging every echo a fixed 16 ms batch delay. Bursts retain their
trailing timer and 8 KiB message bound. Incomplete UTF-8 tails are retained across
PTY reads so immediate publication cannot split a Unicode character in JSON.

The renderer's ready event binds its native view tag. The shared native module
dispatches scripts on the Android UI queue / iOS main queue directly to that
view's WebView subtree, avoiding Fabric's mounting-frame command queue. It never
searches other screens. Generation guards reject queued scripts after a reload;
late dispatch failures cannot invalidate a replacement renderer. A current
transport failure exposes the existing retry UI, which requests a new full base.
This change requires the matching native app build on both platforms.

The DOM renderer retains unchanged rows. Selection and native dragging defer
painting the touched subtree; the existing non-interactive wheel layer can paint
independently. When interaction ends, both layers can catch up from the current
bounded grid without replaying intermediate frames. Wheel forwarding, bounded
backpressure, exact-row interpolation, and native history virtualization retain
their existing behavior.

History decoding retains its 64-row Ghostty instance, 6000-row capture limit,
16 MiB styled-output limit and cancellation check before each page. It yields
after 4 ms of page work instead of waiting for an RN timer after every page;
the budget is checked between native calls, so one slow page can exceed 4 ms.
The initial/idle capture debounce remains 750 ms. Loading-history measurements
must include that debounce and native decoding, not just WebView DOM insertion.

Focused behavioral tests live beside the renderer, outside runtime routes.
`scripts/test-terminal-row-updates-android.sh DEVICE_SERIAL` exercises the shared
native formatter on an already running Android device. It reconstructs hundreds
of delta frames and compares each with the full formatter, covering cursor-only
updates, ANSI/CJK, wrapping, alternate screens, clear/redraw, resize, and theme.
Performance acceptance additionally requires device traces for input latency,
flood responsiveness, frame pacing, and scrolling; unit tests do not certify those
budgets. iOS native compilation and device checks require an Apple build host.

Brain host and ordinary Worker terminal mode share `TerminalOutputPane` →
`TerminalSurfaceGhosttyWebView` → `useGhosttyTerminalController`. Structured chat
is a separate overlay, removed in terminal mode. The identity of the Worker does
not choose the scroll owner: pane modes do. Plain-screen output uses local
WebView history; an alternate-screen pane requesting mouse reporting receives
bounded wheel input through the authenticated terminal session. The latter
necessarily includes a PTY/application redraw round trip. See
[terminal scrolling](terminal-scrolling.md) for that contract and its limits.
