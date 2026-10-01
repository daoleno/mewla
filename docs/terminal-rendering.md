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
Output callbacks retain the existing coalescing policy. The WebView accumulates
every delta and paints the latest state once per animation frame. Wheel output
uses the existing immediate task to avoid an additional RN animation-frame wait.

The DOM renderer retains unchanged rows. Selection and native dragging defer
painting the touched subtree; the existing non-interactive wheel layer can paint
independently. When interaction ends, both layers can catch up from the current
bounded grid without replaying intermediate frames. Wheel forwarding, bounded
backpressure, exact-row interpolation, and native history virtualization retain
their existing behavior.

Focused behavioral tests live beside the renderer, outside runtime routes.
`scripts/test-terminal-row-updates-android.sh DEVICE_SERIAL` exercises the shared
native formatter on an already running Android device. It reconstructs hundreds
of delta frames and compares each with the full formatter, covering cursor-only
updates, ANSI/CJK, wrapping, alternate screens, clear/redraw, resize, and theme.
Performance acceptance additionally requires device traces for input latency,
flood responsiveness, frame pacing, and scrolling; unit tests do not certify those
budgets. iOS native compilation and device checks require an Apple build host.
