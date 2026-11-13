# Agents' browser on the virtual screen

There is one browser tool for every Worker on every computer: `mewla_chrome`,
Playwright's browser tools driving Chrome through the Mewla extension. The only
difference between computers is where the agent's Chrome window lives:

- **A Mac, or a desktop without the virtual screen:** the person's own Chrome, as
  [your-browser.md](../your-browser.md) says.
- **Linux with Settings → Agents → Virtual screen on:** the **agent browser**: one
  Chrome on one private profile (`~/.mewla/browsers/agent`), running on the
  **agent screen**, carrying the same Mewla extension and joined through the same
  native host and bridge. A Worker gets a **window of its own** in that Chrome,
  on **its monitor** of the agent screen, and the person watches or takes over
  that monitor from **Screen** on the phone.

The switch is kept on the computer (`~/.mewla/browsers/settings.json`), so every
device sees the same answer. With it on, a Worker launched by Mewla gets
`mewla_chrome` and nothing else for the web; there is no second browser tool.

## The agent screen

Linux has one virtual-screen substrate, `daemon/vdisplay`: one `Xvnc` (X11) for
the computer, started when the first agent needs it, with a RandR monitor for
each agent that has something on it, plus a parking monitor no agent owns.
Chrome is not special: it is one more program on that screen, like the desktop
apps an agent opens with its display tool. There is no kwin, no Wayland session
and no screen streaming of Chrome's own.

- **Monitors.** Each is 1280×800. Slot 0 is the parking monitor; the agents' are
  laid out left to right after it, and the layout is changed with the RFB
  `SetDesktopSize` message when an agent comes or goes (so there is no `xrandr`
  dependency). The monitor keeps its slot while its Worker lives. It is given up
  when the Worker ends, or when it has neither a browser window nor a program left.
- **Windows.** There is no window manager. A new X window appears at the screen's
  corner, on the parking monitor, so the daemon moves it: a desktop app's window
  is moved by a mover that watches the screen's top-level windows and finds the
  program's process (X RES, or `_NET_WM_PID`) in the process group an agent's
  `open` started; Chrome's windows are placed by the extension (below). Chrome
  knows the monitors: its menus and pop-ups stay inside the one they are on. A
  window state of "maximized" needs a window manager, so a window is sized to the
  monitor's rectangle instead.
- **Input.** The pointer and the keyboard focus are the screen's, so one input
  action runs at a time, and before an agent's keys the pointer returns to where
  that agent left it, on its own monitor. A person's viewer has its pointer moved
  into its own monitor.
- **The view.** A viewer is served by `vdisplay`'s own RFB server for one
  rectangle of the screen (`crop.go`): it is told the screen is the monitor's
  size, and what it is sent are the monitor's changed tiles (ZRLE, or Raw for a
  viewer without it). Nothing outside the monitor is ever sent. Clipboard text
  the person sends goes to the screen's clipboard; none is sent back.

## The extension in the agent browser

Branded Google Chrome 137 and later ignore `--load-extension`, and nothing may
be asked of the person: computer use is core. The daemon starts Chrome with
`--remote-debugging-pipe --enable-unsafe-extension-debugging` and sends
`Extensions.loadUnpacked` over the pipe, which only the pipe accepts. The
extension loads with the same ID its manifest key fixes
(`keclgeaemhlgnnhocopjklenhgfimmjh`), so the native host's allow-list holds. It
is loaded again at every start, so it is always the daemon's own version; nothing
is written to the profile but the native host manifest in
`<profile>/NativeMessagingHosts`, which points to a launcher that runs
`mewla agent-use native-host --agent`. The pipe is the daemon's end of Chrome's
debugging pipe: if the daemon goes, Chrome goes with it. It is also how Chrome is
asked to quit (`Browser.close`), so the profile flushes. Chrome has no debugging
port.

## One Chrome, a window per agent

Chrome allows one process per profile (a second one hands its request to the
first, on the first one's display), so agents share one Chrome and one set of
sign-ins, each in its own window on its own monitor.

- The first tool call of a Worker makes the shim Playwright runs in place of
  Chrome (`mewla agent-use open`) ask the daemon's bridge to connect. With the
  virtual screen on, the bridge starts the screen and Chrome on the profile and
  **waits for it**; agents that ask while it is starting wait for the same start.
  It asks `vdisplay` for the Worker's monitor and tells the extension the
  rectangle. A window ignores the position it is created at on a screen with no
  window manager, so the extension creates it and then moves it (`windows.update`);
  a window one of the agent's pages opens (a sign-in pop-up) is moved to the
  same place. The extension connects the window to that Worker's Playwright server.
- A window closes when its Worker ends (its connection closes, the extension
  closes the window and says so) or when the daemon's sweep finds the Worker
  gone. Chrome stops when no window is left; the agent screen stays while
  any Worker has a monitor; the profile and its sign-ins stay.
- A failure to start names its cause to the agent and in `daemon.log`: what
  Xvnc or Chrome wrote to stderr, or that it exited, never a bare "unavailable".

## Signing in once

The profile belongs to the agents, not to the person's everyday Chrome. To give it
a sign-in, take over an agent's monitor in Screen, sign in, and let go. The
sign-in is kept on the computer and every agent's window uses it, since they are
windows of one Chrome, also after the browser stops. Site expiry and
reauthentication still apply.

Chrome keeps the profile's secrets in the Secret Service (`--password-store=gnome-libsecret`)
when the server user's default collection exists and is unlocked, and otherwise in
Chrome's own basic storage inside the profile folder, readable only by that user.
A profile keeps the store it started with (`credential-store` in the profile
root), since cookies written under one cannot be read under the other. Mewla never
activates, unlocks or reads a wallet.

## Screen

**Screen** (Linux) lists the agents currently on the agent screen: a row for each,
with its monitor's live view and **Hold the agent**, **Let the agent go on** and
**Stop**. The Phone is a row in the same menu. There is no New browser, Open
browser or Delete. A browser window and an agent's desktop apps are on the same
monitor, so they are one row. Showing a row is Watch: taps and typing reach the
screen without pausing the agent, which is told what happened (clicks, how much
was typed, never the text) and judges for itself. Hold and Stop are the hard
parts. Chrome's own address bar and tabs are on the screen, so there is no
address bar or tab strip of Mewla's.

## The agent's calls and the person's say

Playwright's tools are not changed, and the daemon is not in their path, so the
person's say reaches them at the stdio layer: `mewla agent-use chrome-mcp` runs a
small supervisor between the agent's MCP host and Playwright. Around each
`browser_*` call it asks the daemon (over the private control socket), which
answers from the Worker's monitor, the same state the display tool answers from:

- **begin** is refused with the reason while the person holds the monitor (Hold)
  or has stopped the Worker; the call never reaches Playwright. A Worker with no
  monitor yet is not refused: its first call makes one. Hold applies to new
  calls; one already running is not waited for.
- **end** returns what the person did on the monitor since the Worker last heard:
  clicks, scrolls, how much was typed (never what), and, after the person lets go,
  when they had it. The supervisor adds it to the result as one more block of
  text. Person input never pauses an agent: it is told, and judges.

The same supervisor adds `browser_read` (the page's text and address, up to 40000
characters) and advice on a few tools' descriptions, and gives Playwright a
12-second navigation timeout so a page that never finishes loading does not hold
the agent. On a computer with no virtual screen there is no window to hold, so the
calls pass at once and the Mac behaves the same way.

## Runtime and host

The Linux adapter reuses installed Google Chrome or Chromium on the agent screen
(`Xvnc` and `xauth`). It does not use the physical seat, modify the display
manager or replace the desktop, and it never adopts a personal Chrome profile.
The supervisor serializes launches and holds an OS lock on the profile. Chrome
owns its normal profile lock; Mewla never deletes SingletonLock. Every way the
browser ends (idle, the daemon's shutdown) asks Chrome for `Browser.close` first,
so the profile flushes; a daemon that is killed loses the sign-ins made since
Chrome last flushed. Session-only cookies, live DOM and tab identity are not
promised after a Chrome restart.

**Security scope:** the daemon principal is the server owner represented by paired
devices, not a multi-user login model. Agent access is limited to the same server
and owner, but the local control socket is open to the owner UID. Managed tools
enforce Hold and Stop; an arbitrary same-UID shell can reach the screen's socket
or read the profile and bypass them. Loopback, Unix sockets and mode-0700
directories are not a sandbox, and the browser must not be presented as protecting
logins from an untrusted same-UID agent with unrestricted shell access. The agents
share one X server, so a program of one agent can read another's windows; the
monitors are for showing and for the person's input, not isolation between
agents.

## The app's side

`POST /browser` (purpose `mewla-browser`) answers `settings` only: the Virtual
screen switch. `GET /display` lists one entry per agent (the same agent id the
viewer uses) with `reason` when the computer cannot run the screen or Chrome, and
`/display/viewer?worker=<id>` is the live view, the monitor's crop.

## Verification

Run `go test -race ./agentuse ./browser ./vdisplay`, and `cd client/ui && nsh check`. Behavioral tests cover the monitors (a program
opens on its Worker's monitor, a viewer is shown only that rectangle, a monitor
goes with its Worker and the others keep their places), the gate (refusal with a
reason, notes with the result), a concurrent cold start (the second agent waits
for the first start) and the host's bookkeeping.

The whole chain (two Workers, Screen, Hold, typing, Let go) is
`scripts/bench/agent-use/wav/vscreen.sh` with real `claude -p` agents; with a page
that sets a cookie, a second Worker (and one after the browser has stopped and
started again) sees the first one's sign-in.
