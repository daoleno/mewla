# Agents' browser on the virtual screen

There is one browser tool for every Worker on every computer: `mewla_chrome`,
Playwright's browser tools driving Chrome through the Mewla extension. The only
difference between computers is where the agent's Chrome window lives:

- **A Mac, or a desktop without the virtual screen:** the person's own Chrome, as
  [your-browser.md](../your-browser.md) says.
- **Linux with Settings → Labs → Virtual screen on:** the **agent browser**: one
  Chrome on one private profile (`~/.mewla/browsers/agent`), shown on a virtual
  screen that is not the person's desktop, carrying the same Mewla extension and
  joined through the same native host and bridge. A Worker gets a **window of its
  own** in that Chrome, and the person watches or takes over that window from
  **Screen** on the phone.

The switch is kept on the computer (`~/.mewla/browsers/settings.json`), so every
device sees the same answer. With it on, a Worker launched by Mewla gets
`mewla_chrome` and nothing else for the web; there is no second browser tool.

## The extension in the agent browser

Branded Google Chrome 137 and later ignore `--load-extension`, and nothing may
be asked of the person: computer use is core. The daemon starts Chrome with
`--remote-debugging-pipe --enable-unsafe-extension-debugging` (and the debugging
port the live view uses), and sends `Extensions.loadUnpacked` over the pipe,
which only the pipe accepts. The extension loads with the same ID its manifest
key fixes (`keclgeaemhlgnnhocopjklenhgfimmjh`), so the native host's allow-list
holds. It is loaded again at every start, so it is always the daemon's own
version; nothing is written to the profile but the native host manifest in
`<profile>/NativeMessagingHosts`, which points to a launcher that runs
`mewla agent-use native-host --agent`. The pipe is the daemon's end of Chrome's
debugging pipe: if the daemon goes, Chrome goes with it.

## One Chrome, a window per agent

Chrome allows one process per profile, so agents share one Chrome and one set of
sign-ins, each in its own window.

- The first tool call of a Worker makes the shim Playwright runs in place of
  Chrome (`mewla agent-use open`) ask the daemon's bridge to connect. With the
  virtual screen on, the bridge starts the runtime (a private KWin virtual Wayland
  session, then Chrome on the profile) and **waits for it**; agents that ask while
  it is starting wait for the same start. The extension then opens a window
  (1280×800, over the others) and connects it to that Worker's Playwright server.
- The extension tells the daemon the page each window shows (`view`), so the
  Screen row follows the active tab. The row, its lease, the person's touches and
  Stop belong to the window, keyed by the Worker.
- A window closes when its Worker ends (its connection closes, the extension
  closes the window and says so) or when the daemon's sweep finds the Worker
  gone. Chrome and the virtual session stop when no window is left; the profile
  and its sign-ins stay.
- A failure to start names its cause to the agent and in `daemon.log`: what KWin or
  Chrome wrote to stderr, or that it exited, never a bare "unavailable".

## Signing in once

The profile belongs to the agents, not to the person's everyday Chrome. To give it
a sign-in, take over an agent's window in Screen, sign in, and hand back. The
sign-in is kept on the computer and every agent's window uses it. Site expiry and
reauthentication still apply.

Chrome keeps the profile's secrets in the Secret Service (`--password-store=gnome-libsecret`)
when the server user's default collection exists and is unlocked, and otherwise in
Chrome's own basic storage inside the profile folder, readable only by that user.
A profile keeps the store it started with (`credential-store` in the profile
root), since cookies written under one cannot be read under the other. Mewla never
activates, unlocks or reads a wallet.

## Screen

**Screen** (Linux) lists the agents currently on the virtual screen: a row for
each window, with its live view and **Take over**, **Hand back** and **Stop**. The
Phone is a row in the same menu. There is no New browser, Open browser or Delete.
Showing a row is Watch: taps and typing reach the page without pausing the agent,
which is told what happened (clicks, how much was typed, the page address, never
the text) and judges for itself. Take over and Stop are the hard parts. A dropped
view reconnects up to three times (1 s, 2 s, 4 s), then offers Reconnect. Leaving
the page releases the phone's viewer and keeps the window, its agent and the
sign-ins.

The live view is Chrome's own screencast of the page the window shows (JPEG frames,
one outstanding until the renderer's acknowledgement, so the viewer's pace is the
page's pace). A headed window's page is smaller than the screen once Chrome's own
toolbar takes its share; pointer coordinates come from the frame's metadata.

## The agent's calls and the person's say

Playwright's tools are not changed, and the daemon is not in their path, so the
person's say reaches them at the stdio layer: `mewla agent-use chrome-mcp` runs a
small supervisor between the agent's MCP host and Playwright. Around each
`browser_*` call it asks the daemon (over the private control socket):

- **begin** is refused with the reason while the person holds the window (Take
  over) or has stopped the Worker; the call never reaches Playwright. Take over
  refuses new calls at once and waits for the ones running, up to ten seconds, since
  a call cannot be cancelled from here.
- **end** returns what the person did in the window since the Worker last heard:
  clicks, scrolls, how much was typed (never what), the page address, and, after
  Hand back, when they had the window. The supervisor adds it to the result as one
  more block of text. Person input never pauses an agent: it is told, and judges.

The same supervisor adds `browser_read` (the page's text and address, up to 40000
characters) and advice on a few tools' descriptions, and gives Playwright a
12-second navigation timeout so a page that never finishes loading does not hold
the agent. On a computer with no virtual screen there is no window to hold, so the
calls pass at once and the Mac behaves the same way.

## Runtime and host

The Linux adapter reuses installed Google Chrome or Chromium and packaged KWin's
virtual Wayland backend (`kwin_wayland --virtual`). It does not start Plasma, use
the physical seat, modify SDDM or replace the desktop, and it never adopts a
personal Chrome profile. The supervisor serializes launches and holds an OS lock
on the profile. Chrome owns its normal profile lock; Mewla never deletes
SingletonLock. Closing asks Chrome for `Browser.close` first so the profile
flushes. Session-only cookies, live DOM and tab identity are not promised after a
Chrome restart.

**Security scope:** the daemon principal is the server owner represented by paired
devices, not a multi-user login model. Agent access is limited to the same server
and owner, but the local control socket is open to the owner UID, and the agent
browser's debugging port is on loopback. Managed tools enforce Take over and Stop;
an arbitrary same-UID shell can discover the port or read the profile and bypass
them. Loopback and mode-0700 directories are not a sandbox, and the browser must
not be presented as protecting logins from an untrusted same-UID agent with
unrestricted shell access.

## Viewer transport

Management is signed `POST /browser` (purpose `mewla-browser`) with `list`,
`status`, `settings` and `halt` only. A separate authenticated
`/browser/viewer?id=<window UUID>` WebSocket is signed with purpose
`mewla-browser-view:<UUID>`; control, release, command (the person's input, and
the address bar's and tab strip's actions), touch and halt go over it. Raw
screencast and CDP stay on private loopback ports. A frame has at most 2.8 MB of
image data and the proxy sends one outstanding frame until the renderer's ACK;
control replies continue while a frame awaits ACK, and bounded write deadlines
disconnect stalled readers.

## Verification

Run `go test -race ./agentuse ./browser`, the app typecheck and both Android and
iOS Expo exports. Behavioral tests cover scope, revoke and disconnect, bounded
frames, concurrent cold start (the second agent waits for the first start), the
gate (refusal with a reason, notes with the result, drain), and the extension
loading into the agent browser.

The opt-in real proofs never use personal accounts:

    MEWLA_BROWSER_INTEGRATION=1 go test ./browser -run TestAgentChromeCarriesExtension -v -count=1

starts a real virtual screen and Chrome twice on one profile and checks the
extension loads with its ID and Chrome starts the native host. The whole chain
(two Workers, Screen, Take over, Hand back) is `scripts/bench/agent-use/wav/vscreen.sh`
with real `claude -p` agents.
