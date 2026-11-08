# Agents' browser on the virtual screen

Every computer has **one agent browser**: one Chrome on one private profile
(`~/.mewla/browsers/agent`), shown on a virtual screen that is not the person's
desktop. The person never creates, opens, names or deletes browsers. An agent
(a Claude or Codex Worker) gets a **window of its own** in that Chrome, and the
person watches or takes over that window from **Screen** on the phone.

The switch is **Settings → Labs → Virtual screen**, kept on the computer
(`~/.mewla/browsers/settings.json`), so every device sees the same answer. With
it on, a Worker launched by Mewla gets the `browser` tool without being told
which browser; with it off, none is attached.

## One Chrome, a window per agent

Chrome allows one process per profile, so agents share one Chrome and one set of
sign-ins, each in its own window.

- The first `control` from a Worker starts the runtime (a private KWin virtual
  Wayland session, then Chrome on the profile) and **waits for it**. Agents that
  ask while it is starting wait for the same start; nobody is told "unavailable"
  because something was stopped.
- That Worker then gets a new window (`Target.createTarget` with `newWindow`)
  and a private agent-browser session pinned to it (`--pin-tab`). Sessions on one
  Chrome do not share an active tab once pinned, so agents act at the same time
  without moving each other's pages. Each session has its own viewer stream.
- Lease, owner, halted and `needs_restart` state, the person's touches and the
  Screen row belong to the agent's window, not to Chrome.
- A window closes when its Worker ends. Chrome and the virtual session stop when
  no window is left; the profile and its sign-ins stay.
- An action that could not be drained (the window may still be running it)
  marks that window uncertain. The agent's next `control` closes just that
  window and opens a fresh one; other agents' windows are not touched. If Chrome
  itself dies, the next `control` of each agent starts it again.
- A failure to start names its cause to the agent and in `daemon.log`: what
  KWin or Chrome wrote to stderr, or that it exited, never a bare "unavailable".

## Signing in once

The profile belongs to the agents, not to the person's everyday Chrome. To give
it a sign-in, take over an agent's window in Screen, sign in, and hand back. The
sign-in is kept on the computer and every agent's window uses it. Site expiry and
reauthentication still apply.

Chrome keeps the profile's secrets in the Secret Service (`--password-store=gnome-libsecret`,
the standard libsecret client; KWallet 6's `ksecretd` works) when the server
user's default collection exists and is unlocked, and otherwise in Chrome's own
basic storage inside the profile folder, readable only by that user. A profile
keeps the store it started with (`credential-store` in the profile root), since
cookies written under one cannot be read under the other. One that began on the
wallet waits for the wallet instead of losing its sign-ins. Mewla never
activates, unlocks or reads a wallet. The `browserfixture` build tag forces basic
storage for disposable synthetic tests; never build a release with it.

## Screen

**Screen** (Linux) lists the agents currently on the virtual screen: a card for
each window, with its live view and **Take over**, **Hand back** and **Stop**,
and a card for each agent display. The Phone is a row in the same menu. There is
no New browser, Open browser or Delete: the card of an agent that has not asked
for a browser does not exist yet. Showing a card is Watch: taps and typing reach
the page without pausing the agent, which is told what happened (clicks, how
much was typed, the page address, never the text) and judges for itself. Take over
and Stop are the hard parts. A dropped view reconnects up to three times (1 s,
2 s, 4 s), then offers Reconnect. Leaving the page or backgrounding the app
releases the phone's viewer and keeps the window, its agent and the sign-ins.

## Runtime and host

The Linux adapter reuses installed Chrome/Chromium, agent-browser **0.38.1** and
packaged KWin's virtual Wayland backend (`kwin_wayland --virtual`). It does not
start Plasma, use the physical seat, modify SDDM or replace the desktop, and it
never adopts a personal Chrome profile. The page viewport is 1280×800 and is
configured through agent-browser when a window opens and when a tab is selected,
so the encoded frame and input coordinates stay aligned. This is a page
viewport, not a desktop: native Chrome chrome, file pickers, extension windows
and device-bound passkeys are outside the viewer. Google/MFA acceptance is not
established.

The supervisor serializes launches and holds an OS lock on the profile. Chrome
owns its normal profile lock; Mewla never deletes SingletonLock. Closing asks
Chrome for `Browser.close` first so the profile flushes. Session-only cookies,
live DOM and tab identity are not promised after a Chrome restart.

## Agent tool

The shared broker tool is named `browser`. Call `control`, then
snapshot/read (the page's text)/tabs/select_tab/navigate/new_tab/click/fill/press or the dialog
actions; click and fill references come from the current snapshot; `release`
detaches. Raw CDP, arbitrary evaluation and profile paths are never returned.
Mewla injects the tool into that launch's stdio MCP configuration only, with the
real managed Worker identity; global provider configuration is not changed. A
command that cannot take it launches without it. Operations verify the owned live
pane and process, and a finished delegated Worker is refused. The MCP process
releases on EOF/termination and checks liveness on every action and heartbeat; a
dead process's lease expires after 30 seconds. A Worker is identified by its
Worker ID, so a daemon restart does not detach running Workers.

Only one managed input owner exists per window. Human takeover fences new actions
and waits for the outstanding one to drain before it is acknowledged; results from
an agent action are suppressed once takeover is requested, and agent reads,
including snapshots, are denied during human control. Unknown completion marks
the window `needs_restart`; no false promise of cancellation lets credentials race
unfinished automation. Leases carry epoch, process generation and exact target.
Old queued mobile commands are discarded on control changes. Another agent cannot
preempt the owner. Disconnect, backgrounding, current-server switch and device
revocation release held keys and buttons.

**Security scope:** the daemon principal is the server owner represented by paired
devices, not a multi-user login model. Agent access is limited to the same server
and owner, but the local control socket is open to the owner UID. Managed tools
enforce leases; an arbitrary same-UID shell can discover loopback CDP or read the
profile and bypass them. Loopback and mode-0700 directories are not a sandbox,
and the browser must not be presented as protecting logins from an untrusted
same-UID agent with unrestricted shell access.

## Viewer transport

Management is signed `POST /browser` (purpose `mewla-browser`) with `list`,
`status`, `settings` and `halt` only. A separate authenticated
`/browser/viewer?id=<window UUID>` WebSocket is signed with purpose
`mewla-browser-view:<UUID>`; control, release, command, touch and halt go over it.
Raw stream and CDP stay on private loopback ports. A frame has at most 2.8 MB of
image data and the proxy sends one outstanding frame until the renderer's ACK;
control replies continue while a frame awaits ACK, and bounded write deadlines
disconnect stalled readers. Credentials are never logged by the Browser handler.
The stream uses the existing Mewla Link TCP/TLS, LAN or Tailscale selection and
needs no TURN server.

## Verification

Run `go test ./...`, the app typecheck and both Android and iOS Expo exports.
Behavioral tests cover scope, revoke and disconnect, bounded frames, concurrent
cold start (the second agent waits for the first start), stale generation, epoch
and target, control drain, and recovery of an uncertain window.

The opt-in real synthetic proof never uses personal accounts:

    MEWLA_BROWSER_INTEGRATION=1 go test -tags browserfixture ./cmd/mewla \
      -run '^TestBrowserFirstProductFlow$' -v -count=1

It drives a headed private runtime, the authenticated product viewer and a real
stdio MCP through the product Unix broker. A deterministic launch is not a
successful real-provider task; Google login, hardware MFA and iOS interaction
remain unverified.
