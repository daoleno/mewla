# Persistent Browser (candidate)

Browser is a user-owned named resource on the **current Zen server**. Opening
the `/browser` route opens the default browser on that server (the remembered
one, else a running one, else the oldest). The Browser entry is temporarily
hidden from the navigation drawer; its route and server resources remain. Tapping **Open browser** the first time
creates one named "Browser"; there is no naming or permission step. Sign in on the
host through the viewer. Agent tasks on the same server can use that browser: there
is no per-Agent or per-brand permission switch, and leases only resolve who is
typing at a given moment. Closing the viewer or finishing a task releases input; it does not stop the
browser or delete its cookies. Stop ends the runtime. Delete profile is a separate
confirmed destructive operation. Use different names/profiles for accounts or
projects that must remain separate. Site expiry and reauthentication still apply.

The phone WebView renders remote pixels. The target website and its cookies exist
in the host browser. Navigation, new tabs, tab selection, pointer/scroll input,
text entry and explicit JavaScript dialog handling use the same managed instance.
The first adapter configures a 1280×800 page viewport through agent-browser on
launch and tab selection, keeping the encoded frame and input coordinates aligned
even when a headed window's decorations reduce its initial content area.
This is a page viewport, not an entire desktop: native browser chrome, file pickers,
extension windows and device-bound passkeys are outside the current viewer.
Google/MFA acceptance has not been established by the local synthetic login test.

The Browser page is the viewer: the page view, an address bar with Tabs and keys,
and nothing else on the main surface. Switching browsers, New browser, Stop
controlling, the page dialog, Close browser, confirmed Delete and Connection
details are in the header options menu. The app asks for input only after the
connection's first fresh state shows nobody in control. Input is enabled only once
the server grants a lease. When an Agent or another device holds control, the page
shows who is using it and **Take over**. A dropped view reconnects automatically up
to three times (1 s, 2 s, 4 s), then offers one Reconnect action. Leaving the page
or backgrounding the app releases this phone's lease and keeps the browser, its
sign-ins and any task running. Failures (server without
Browser, pairing rejected, unreachable server, control taken, restart required,
non-JSON responses) are shown as readable cards with one recovery action; the raw
message and HTTP status stay behind a collapsed Details row.

## Runtime and host

The first Linux adapter reuses installed Chrome/Chromium, agent-browser **0.38.1**
and packaged KWin's virtual Wayland backend. It does not start Plasma, use the
physical seat, modify SDDM, or replace the host desktop. A general-purpose minimal
base OS, an optional packaged graphical session, Browser resources, and the
remote-view transport are separate layers. A future packaged session/maintained
engine can replace the browser.Backend and browser.Stream adapters without
changing resource IDs, pairing or Agent leases. This is not an Omarchy port,
desktop customization system or distro installer.

Profiles live only under the daemon's private browsers/<UUID>/profile. The
supervisor serializes launches and holds an OS lock for each profile, tracks a new
process generation after restart, and never adopts a personal Chrome profile.
Chrome also owns its normal profile lock; Zen never deletes SingletonLock.
A private display and compositor bus are owned by the runtime. Chrome uses the
server user's existing stable session bus for Secret Service. Closing Chrome
requests Browser.close to flush persistent state before process cleanup.

Production launch requires an already running, unlocked default Secret Service
collection and requests --password-store=gnome-libsecret. This historical Chromium
flag selects the standard Secret Service adapter; it does not require a GNOME
desktop. The installed KWallet 6 Secret Service (ksecretd) passed an isolated,
encrypted-wallet test using the production backend: Chrome stored the synthetic
persistent cookie as platform-key-encrypted v11 data, and clean browser restart
retained authentication. Zen does not activate, unlock, read or change a user's
wallet automatically and has no basic-store fallback. On this server the ordinary
user must start/unlock their supported wallet before Browser becomes available.
The disposable test does not establish unattended unlock of a real user wallet.
The browserfixture build tag exposes an explicitly unsafe store only for
disposable synthetic tests; never build a release with this tag. Session-only
cookies, live DOM and tab identity are not promised after process restart/crash.
Unexpected process exit makes the resource unavailable until reopened; profile
recovery still belongs to Chrome.

## Managed Agent attachment

Open browsers on the current server are listed in the normal new-session sheet.
The remembered default browser is preselected when it is running and shared with
Agents. A preselected browser is sent only with commands that can attach it (Codex
or Claude); other commands launch without it. A browser chosen explicitly is always
sent, so an unsupported command reports why. Opening a browser from the app also grants it to Agents on
this server once, so browsers created before this default keep working with the
existing grant check. No global provider/MCP
configuration is changed. Zen injects the selected resource into that invocation's
stdio MCP configuration and supplies the real managed task identity. Unsupported
providers or conflicting command-level MCP overrides return an explicit error.
The CLI equivalent is `zen worker spawn --browser PROFILE_UUID` with the usual
session options. Opening or finishing a task never creates/deletes a profile.

The shared broker tool is named browser. Call control, then
snapshot/tabs/select_tab/navigate/new_tab/click/fill/press or the dialog actions.
Click/fill references come from the current snapshot. Call release to detach.
Raw CDP, arbitrary evaluation and profile paths are not returned. A task can use
only its selected Browser. Managed operations verify the owned live pane/process;
delegated task completion rejects further operations even while its process stays
alive. Quiet manual providers need not paint terminal output to remain active.
The MCP process releases on EOF/termination and checks task liveness on every
action/heartbeat. A dead process lease expires after 30 seconds (expiry scan every
3 seconds). Browser runtime and persisted profiles outlive attachment. After a
daemon restart, attachments fail closed: reopen the Browser and create a new
session selecting it. Existing task bindings are not currently restored.

Only one managed input owner exists at a time. Human takeover first fences new
actions and waits for the outstanding action to drain before acknowledgement.
Results from an Agent action are suppressed once takeover is requested. All
managed Agent reads, including snapshots, are denied during human control.
Unknown action completion marks the browser needs_restart; no false promise of
cancellation allows human credentials to race unfinished automation. Leases carry
epoch, process generation and exact target. Old queued mobile commands are
discarded on control changes. Another Agent cannot preempt the current owner.
Human disconnect, backgrounding, current-server switch and device revocation
release held keys/buttons. Reconnect views the same browser and requests input only
when the fresh state after reconnecting shows the browser idle. It never preempts an
Agent or device that took control in the meantime.

**Security scope:** the existing daemon principal is the server owner represented
by paired devices, not a new multi-user login model. Agent access is limited to
the same server and owner, but the local control socket is accessible to the owner UID. Managed
tools enforce leases; an arbitrary same-UID shell can discover loopback CDP or
read the profile and bypass them. Loopback and mode-0700 directories are not a
sandbox. Separate UID/network namespace/container execution would be needed for
hostile shell isolation; that has not been implemented or claimed here. The
resource must not be presented as protecting human logins from an untrusted
same-UID Agent with unrestricted shell access.

## Viewer transport

Management is signed POST /browser using the paired-device purpose
zen-browser. A separate authenticated /browser/viewer?id=<UUID> WebSocket is
signed with purpose zen-browser-view:<UUID>. Raw stream/CDP remain private
loopback endpoints. One viewer per resource is supported initially. Revoked
devices cannot reconnect. A frame has at most 2.8 MB encoded image data, and the
proxy sends only one outstanding frame until the renderer's ACK. The
agent-browser upstream retains the latest frame while waiting. Control replies
continue while a frame awaits ACK; bounded write deadlines disconnect stalled
readers. Credentials are never logged by the Browser handler.

The image stream is routed through the existing Zen Link TCP/TLS, LAN or Tailscale
address selection; consult the evidence report for the paths actually exercised. It needs no TURN server. Replacing it with WebRTC would require
ICE/UDP and possibly TURN; Zen's opaque TLS relay is not TURN. A viewport stream
does not provide a full graphical desktop.

## Verification and remaining gates

Run go test ./..., app typecheck and both Android/iOS Expo exports. Behavioral
tests cover paired resource scope, revoke/disconnect, normal close/reconnect,
bounded frames, lifecycle independence, launch serialization, stale generation/
epoch/target, control drain and uncertain actions.

Opt-in real synthetic proof (never uses personal accounts):

    ZEN_BROWSER_INTEGRATION=1 go test -tags browserfixture ./cmd/zen \
      -run '^TestBrowserFirstProductFlow$' -v -count=1

It uses a headed private runtime, authenticated product viewer, later real stdio
MCP attachment through the product Unix broker, detach/reattach, popup/dialog,
reconnect, and persistent-versus-session-cookie restart. The default task liveness
is a fixture. `ZEN_BROWSER_PROVIDER=probe` adds two deterministic protocol clients
through the normal authenticated create_session/provider launch boundary and real
watcher; it makes no model request. `ZEN_BROWSER_PROVIDER=1` calls the configured
real provider and must only be used with an explicit bounded QA prompt budget.
`ZEN_BROWSER_SECURE=1` uses the production backend and requires an isolated unlocked
Secret Service; the test additionally checks encrypted cookie storage.

A successful deterministic launch is not a successful real-provider task. See the
implementation report for provider failures, native Link evidence, secure wallet
setup and platform gaps. Google login, hardware MFA and iOS interaction remain
unverified. No release or live-service cutover is included in this candidate.
