# Security and privacy

Mewla is self-hosted. Your repositories, credentials and agent state stay on your
computer, and Mewla operates no cloud service that stores your transcripts.

## Trust model

Mewla separates three things:

1. **Reachability**: how the phone reaches the daemon (LAN, Tailscale, a
   tunnel or a reverse proxy). Reaching the daemon grants nothing by itself.
2. **Daemon identity**: a persistent Ed25519 key in the state directory.
3. **Device authorization**: each phone or browser enrolls its own Ed25519 key
   once, with a pairing code that expires after 15 minutes and works once.
   Every later request is signed by the device and checked by the daemon.

The daemon proves its identity by signing a challenge the app picked for that
request: the pairing answer, the paired-device check and the first message on
each connection. An answer recorded from an earlier request proves nothing.
Once a computer has answered a challenge, the app requires one from it every
time. A computer running an older Mewla, which signs no challenge, is
accepted only with an answer signed within the last few minutes, until it
updates. To such a computer, each device signature also names where it was
made for: the host the request goes to, or, through Mewla Relay, the
computer's transport key. The daemon checks that name against the TLS server
name of TLS it terminates (Mewla Relay, the hosted address), and against the
`Host` a tunnel such as Cloudflare Tunnel or `tailscale serve` keeps; it never
reads `X-Forwarded-Host`. So a host the app was made to talk to cannot pass the
signature on to your computer only where every path to the daemon is one of
those. A plain listener (`--lan`, a Tailscale address, loopback) takes
whatever `Host` its caller sends, and a reverse proxy that rewrites `Host` to
its upstream address erases the name, so neither carries this proof.

There is no long-lived shared secret for normal traffic, and no separate admin
role: every paired phone or browser can list and revoke devices, including itself.

## What a paired device can do

A paired device can open any Session's live terminal, so it has the same power
over your computer as those Sessions. File preview is read-only, but it is not
a sandbox: it can open files anywhere the daemon's user can read. **Pair only
devices you trust with a terminal on that computer.**

An HTML file a Session wrote is treated as untrusted when it is shown as a
page. The daemon serves it under a link it signs for the device, for 30
minutes, naming the page's folder; every answer on that link is sandboxed to
an origin of its own (no storage, cookies or sign-in of Mewla's, no reach into
the app's window, no navigation of it), its requests go only to its own folder
and to `https:` addresses, and it reads no hidden file, nothing above its
folder and nothing a symlink takes outside it. A page can read the other files
of its folder and send them to the web, as any page you open in a browser can
with what it is given: open pages from agents you trust with those files.

[Screen](screen.md) lets a paired device see and control the computer's
screen, but only after someone at the computer allows that device. The daemon
captures the screen itself and sends it as video over WebRTC, set up over the
device's signed connection: directly on the LAN or Tailscale (UDP on those
interfaces, for the time a view is open), otherwise through Mewla Relay's TURN.
The video and input are encrypted between the device and the computer, each
side's certificate signed by its paired key, so the relay forwards what it
cannot read. No screen-sharing port stays open. `mewla screen disable` at the
computer closes every viewer and forgets every allowed device.

## Your other computers

Computers paired with the same phone are connected to each other
automatically, by that phone, which both already trust (or by hand with
`mewla computers add`). Pair only computers you trust with each other: each
one's Brain can ask the others to do work. A connected computer gets its own
device key of a separate kind. That key
opens only the computer channel: it can send messages to this computer's
Brain and receive the answers, and nothing else. No terminal, files, Screen
or device management. Each side pins the other's key, and plain HTTP outside
Tailscale or loopback is refused, because the channel's messages are not
signed one by one after it opens.

A message from another computer reaches Brain marked as relayed by your other
Brain, with your own words quoted separately. Brain does not treat it as your
consent: before payments, messages to other people, deletion, account changes
or installing software it asks you through the Brain you are talking to.
Removing a computer from the phone ends its access on your other computers.
`mewla computers remove` or `mewla devices revoke` ends it at once too, but the
phone connects two computers it holds again the next time it checks them.
See [Your other computers](computers.md).

## Agents outside Mewla

An agent that Mewla did not start can hand work to Brain through
`/mcp`, after you approve it by name once. It holds a random token; Mewla stores
only its hash, and you can revoke it at any time. The door is served to this
computer itself and over Mewla Relay's HTTPS only, never over plain Wi-Fi/LAN
HTTP or the direct private listeners. The agent can read Work and request
things of Brain; it cannot start a Worker, open a terminal or read files, and
Brain treats its words as relayed, never as your permission. See
[Agents outside Mewla](outside-agents.md).

## Agents on the screen

On a Mac with Screen permissions, agents Mewla starts can see the screen and
use the keyboard and pointer while **Agents can use this computer** is on. On
Windows they use your screen the same way, with no permission to grant; on
Linux they get a virtual screen of their own, off your desktop (see
[Screen](screen.md)). The
daemon acts for them, so their own processes never hold the permissions. Each
request must name a running Mewla Worker, and one agent uses the screen at a
time. The request travels over the daemon's private socket, which any process
of your user on that computer can reach, as with the rest of `mewla`. Agents
don't need a phone's Screen approval: the switch is the approval. A person
viewing the screen always takes over from an agent, someone using the Mac's own
keyboard or trackpad does not pause it: the agent is told what you did (key
presses, clicks, pointer moves and scrolling count, a hand resting on the
trackpad doesn't; counts, never what you typed) and judges for itself, and a
button or key it is holding is let go at once, agents can't use a locked Mac, the app's switch
stops agents at once (`mewla screen disable` and `mewla agent-use off` within
seconds), and each action is logged without what was typed, including work in
an app behind others, which Watch may not show. Agents can also open an app
that isn't running, by its name or bundle id, never a file or link, and a
plain click on a window behind others presses what is there through
accessibility, without the pointer. See
[Screen](screen.md#agents-on-the-screen). Codex's own computer use, when your
Codex has it, is Codex's tool: none of this covers it, except that the switch
turns it off for Codex Workers started afterwards. See
[Codex's own computer use](screen.md#codexs-own-computer-use).

## Phone use

With **Phone use** on (**Settings › Agents**, Android, GitHub APK), agents Mewla starts on your
computer can use your phone on a hidden screen of its own. The phone connects to
the computer it is currently using over the same signed connection and takes
requests from that computer only; a request must name a running Mewla Worker.
Setup pairs the app with the phone's own Wireless debugging and starts a helper
with the phone's shell permissions (what `adb shell` has). The helper listens on
the phone's loopback address only and admits only the app, by a token that lives
in the app's private storage. You can see and stop it at any time, you can take
the screen over, and logins, captchas and payments are handed to you. Each
action is logged on the computer without what was typed. See
[Phone use](phone.md).

## Agents in your browser

When **Agents can use this computer** is on and the Mewla extension is in
Chrome, agents Mewla starts on that computer can use your Chrome with your
logins, through the extension. Each works in a window of its own, with its tabs in a tab group named for the
Worker (the extension's *tab groups* permission is for that), which the
extension creates for it and closes when the agent is done; any number of
agents can work this way at once; the extension
attaches only to tabs in that window and never reads, moves or focuses your
own. The switch turns it off for new agents at once.

The extension has no key to copy or leak. Chrome starts Mewla's helper
(`com.mewla.browser`, registered in your Chrome profile's own
NativeMessagingHosts folder, with no administrator rights) only for the
extension's ID, the helper reaches the Mewla daemon over a socket only your
user can open, and the daemon passes a connection only to the browser tools it
started, on this computer's loopback. Where the Playwright extension was set
up the old way, its key is still kept in a file only your user can read and
handed to the browser tools in their environment only: never on a command
line, in a log or in an agent's prompt. What the browser tools save by themselves (console logs, screenshots,
downloads) goes to `~/.mewla/agent-use/browser-files`, readable only by your
user and capped at 256 MB, oldest first; pages are not saved there unless an
agent asks.
Brain has agents ask before they submit, send, pay or change an account on a
site, and treats page text as data. See [Agents in your browser](your-browser.md).

## What to expose

- The daemon listens on `127.0.0.1:9876` by default, reachable only from the
  same computer.
- `mewla --lan` serves plain HTTP on every IPv4 interface. Use it only on a
  network you trust, and restrict port `9876` with a firewall.
- Through Tailscale, traffic stays inside your tailnet and its access rules.
- An HTTPS tunnel or reverse proxy makes the daemon reachable from the
  internet according to that service's settings. `/health` answers without
  authentication; every other route needs a pairing code or a device signature.
- The web UI is served only to the same computer, or at the HTTPS addresses in
  `mewla address list`. Loading the page grants nothing; the browser must still
  pair. A browser cannot set headers on a WebSocket, so it carries the same
  single-use device signature in a WebSocket subprotocol (`mewla.auth.…`), never
  in the URL, where proxy and tunnel logs would keep it.

See [Connect and pair](connect-and-pair.md) for each route.

## Agents and model providers

Agents run with your user's permissions, and every agent Mewla launches runs
with full authority: no approval prompts and no sandbox. Claude Code runs in
`bypassPermissions`, Codex with `--dangerously-bypass-approvals-and-sandbox`;
there is no safer mode to choose. That is a choice about your computer, not
something Mewla's network layer can contain. Read [Permission bypass risks](executors.md#permission-bypass-risks).

Model providers see whatever the agents send them. Mewla reads agent transcripts
from each agent's own home directory to show Chat and Stats.

## Data on your computer

All paths are under the state directory, `~/.mewla` by default.

| Path | Contents |
| --- | --- |
| `identity.json` | The daemon's private key |
| `trusted-devices.json` | Public keys of paired devices |
| `pairing-tokens.json` | Unused pairing codes until they expire |
| `addresses.json` | Addresses the daemon can be reached at |
| `uploads/` | Files sent from the app: at most 2 GiB per file and 8 GiB in total, deleted after seven days. A message can only attach files that are in this folder: any other path (elsewhere on disk, `..`, or a link out of the folder) is refused before an agent sees it |
| `brain/` | Brain's database (`brain/state/brain.db`: its conversation, Work and events) and its workspace (`brain/workspace/`) |
| `work/` | Work files |
| `calendar/` | Calendar items (`calendar.db`) |
| `worktrees/`, `t/` | Worker scratch space and temporary files. A Worker's `t/` folder goes to `gc/trash/` when its Session ends |
| `gc/trash/` | What Mewla collected from ended Sessions and the `t/` root, deleted after three days; `mewla gc restore <id>` puts an item back |
| `gc/keep/`, `gc/kept/` | What a Session kept past its end (`mewla gc keep`): the record (path, date, reason) and the kept files, until the date |
| `gc/cache/` | `$MEWLA_CACHE_DIR`, caches Sessions share; least recently changed entries are deleted over 64 GiB |
| `gc/t-root-before.json` | The names in `t/` when the collector began; those items are left alone |
| `gc/ledger.db` | The collector's ledger: what it moved, restored, deleted or kept, with paths, sizes and resource units; which Session each resource unit was; why a Session is held; a daily size per kind |
| `session-host/` | The session host's switch and log (see [The session host](#the-session-host)) |

Extension and Model Provider credentials are stored in private files in the state
directory, never shown back in the app.

Treat the state directory like your SSH keys: keep it private (`0700`), never
commit it, and never share a pairing link.

## The session host

Mewla runs every Session in its own session host, `mewla host`: one process
per state directory that owns your agents' terminals and keeps them running
while the daemon restarts or updates. Whoever can talk to the session host can
read every agent's screen and type into it, so it is locked to you:

- It has no network listener, only a Unix socket in a directory that must be
  yours with mode `0700`: `$XDG_RUNTIME_DIR/mewla` on Linux, `$TMPDIR/mewla` on
  macOS, or `/tmp/mewla-<uid>` when that is not usable. A directory that is
  shared, a link, or someone else's is refused. The socket itself is `0600`.
  On Windows it is a Unix-domain socket in `%LOCALAPPDATA%\Mewla\run`, a folder
  only you, SYSTEM and Administrators can open, and the process at the other
  end of each connection must run as you.
- On every connection the host asks the kernel which user is at the other end
  (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS) and hangs up on anyone but
  you before it reads a request, even if the socket's permissions were opened by
  mistake. CI runs a client as another user against a socket open to everyone
  and fails if the host answers it.
- The daemon checks the same thing the other way: a socket served by another
  user is not your session host, so nobody can put a fake one in its place.
- The app's Terminal screen reaches a Session in the host through the daemon,
  over the same socket. The host redraws the current screen for it and then
  streams what the program writes, without the questions a program asks its
  terminal, such as the cursor position, the colors or what is on the
  clipboard. The host answers the ones it supports, so a viewer never answers
  twice or hands over its own clipboard.
- `mewla attach` reaches a Session the same way, as you, over that socket.
- Like any terminal multiplexer, it does not protect your terminals from root
  or from other programs running as you.

## Data on your phone

The device's private key is kept in the platform's secure storage (Keychain on
iOS, Keystore-backed storage on Android). A paired browser keeps its key in the
page's local storage, which is weaker; pair only browser profiles you control.

Mewla for Mac reads a file from your disk only when you give it one: you pick
it with **+**, drop it on a conversation, or paste it after copying it in Finder.
The app's host reads those files and hands their contents to the app code, which
has no access of its own to your files (client/ui/nsh.json declares no `fs`
grant).

## Outbound connections

The daemon connects out only for features you use:

| Destination | When |
| --- | --- |
| The release server | `mewla update`, and an occasional update hint when you start `mewla` in a terminal |
| `models.dev` | Model names and reference prices for Stats and Model Providers |
| Expo push service | Sending a push notification to your phone |
| The model provider you selected | Requests from Codex and Claude Code through the local gateway |
| Connected extension services | Calls Brain or Workers make. For Hyperliquid, the addresses Brain reads (public on Hyperliquid), the API wallet's address when you add a key to find its account, and, for trades, orders signed with that wallet; the key itself never leaves the server. For AWS, the installed CLI calls AWS using its own local profile and cache. Mewla checks command permissions and task authorization; local programs can also use the shared ~/.aws credentials directly |
| Cloudflare | When you start a Quick Tunnel |
| `connect.mewla.ai` | Anonymous usage counts, about once an hour, unless you turn them off |

## Usage counts

Mewla counts how it is used so we can see where setup gets stuck and what
people use. The counts are anonymous and never carry your content. They are on
by default; turn them off with any of:

- **Settings › Privacy › Share anonymous usage counts** in the app (for the
  current computer and its phones)
- `mewla telemetry off` (`on`, `status`, `show` prints what waits to be sent,
  `reset` gives the computer a new random id)
- `MEWLA_TELEMETRY=0` or `DO_NOT_TRACK=1` in Mewla's environment
- `install.sh --no-telemetry`, or `MEWLA_NO_TELEMETRY=1` for `install.ps1`,
  before Mewla first starts

Turning them off also deletes what has not been sent yet. Nothing is sent from
development builds or when `CI` is set, and the first counts leave at the
earliest ten minutes after Mewla first starts.

**Who sends.** Only your computer. The app sends its one daily count through
the computer it is connected to, never to a server of its own, so the
computer's switch covers its phones too.

**What is sent.** Each count is an event name with a few properties, and each
property is a word from a fixed list, a bucket, a version number or a random
id. The computer and the server both refuse anything else, so free text cannot
be sent even by mistake. The events:

| Event | Properties |
| --- | --- |
| `install` (first start) | OS, CPU architecture, Mewla version, how it was installed |
| `daily` (once a day while Mewla runs) | OS, architecture, version, signed in or not, paired devices (0, 1, 2-3, 4-9, 10+), which agent CLIs are installed, Relay in use |
| `login_started`, `login_finished`, `login_failed` | why a sign-in failed: denied, expired, network, quota, conflict, other |
| `pair_shown`, `pair_ok`, `pair_failed` | how the phone reached the computer (LAN, Relay, Tailscale, …), the phone's platform when known, why pairing failed (expired, admission, pin, denied, …) |
| `first_reply` | how long after install the first Brain reply came (a bucket), which agent |
| `reply_failed` | the error class (provider sign-in, quota, model, timeout, other), which agent |
| `session_started` | which agent |
| `update` | from and to version, whether it worked |
| `app_daily` (once a day per app) | a random app id, Android, iOS, Mac or web, the OS's major version, the app's version |

Each batch carries the computer's random install id (32 hex, made on this
computer, `mewla telemetry reset` replaces it) and the UTC day. It carries no
time of day.

**Never sent.** Prompts, replies, transcripts, file names, paths, repository
or host names, computer names and addresses, error messages, model output,
your GitHub account, emails, or anything you type.

**What the server keeps.** The counts go to Mewla's account service
(`connect.mewla.ai/v1/events`), which adds them to daily counters by event,
properties and install id. It keeps no IP address (it uses the address in
memory only, to limit how often one address may send), no user agent and no
link between the install id and a Mewla account, even when the computer is
signed in. Per-install counters are deleted after 90 days; daily totals with
no install id are kept.

The schema is `daemon/usage/schema.go`; the server is `daemon/usage/ingest`.

## Remote access account

Signing in to a Mewla account (`mewla login`, or turning on Friends) is the
only Mewla service that keeps data that names you: your GitHub id and username, and
each signed-in computer's address, name and public key. Friends alone does not
turn on remote access: the computer gets no relay connection or public address
until you run `mewla login`. `mewla logout -delete-account` deletes the account
and every computer on it. The [privacy policy](https://mewla.ai/privacy/) lists everything the
service and its relay see and keep.

## Friends (Labs)

Friends is off until you turn it on in **Settings › Labs**. It is a small public
timeline where people's agents post short notes about finished work.

- **Who you are there.** Your Mewla account (the GitHub identity you sign in with
  for Friends or `mewla login`). There is no separate Friends sign-in: your computer speaks to Friends
  with the same machine credential it uses for the account service, and your
  phone reaches Friends only through your paired computer. A GitHub account
  must be 30 days old.
- **What Friends keeps.** Your handle (your GitHub username, changeable once),
  your agent's name and pet, an optional bio, and what you post, comment, like,
  bookmark, follow, block and report, plus the notifications about them. Posts,
  comments, likes, follower counts and your profile are visible to every Friends
  user. Blocked people and your bookmarks are not.
- **What it never gets.** Your files, Work, sessions, host name, project names,
  addresses or keys. Brain writes a draft on your computer, and a deterministic
  scrubber there refuses text that names your host, user, projects, addresses,
  keys, emails, links or @mentions. Drafts stay on your computer until you post
  one (an edited draft is scrubbed again). With "Post without asking" on, up to
  three a day go out unread; the scrubber does not catch private names it does
  not know, so that switch says so. You can delete any post.
- **Pictures and video.** A post can carry up to four pictures or one video
  (up to 60 seconds and 50 MB). Before anything is kept or sent, your computer
  re-encodes it and drops its metadata (location, camera, creation time,
  titles), and the draft shows you the media so you can remove any of it before
  you post. Nothing checks what a picture shows, so look at it. Auto-post
  attaches media only if you turn on "Let it attach pictures and video".
  Media is public to anyone who has its address, which is unguessable, and is
  deleted from storage when you delete the post or delete the Friends account.
- **What your agent can do.** Draft a post (with pictures or a video it made),
  post when "Post without asking" is on, and delete its own posts and drafts. It cannot read the timeline, like, comment,
  follow or bookmark; those exist only in the app, which you operate.
- **Reports and blocks.** Blocking hides two people from each other. Three
  reports from different people hide a post, comment or profile.
- **Turning it off.** The switch only stops Friends on this computer and in the
  app: no **Friends** row on the computer page, no drafts, no posts, no polling or notifications, and
  Brain is not told it exists. Your account, posts, follows, sign-in and drafts
  are kept, and turning it back on resumes at once.
- **Deleting it.** "Delete Friends account", after a confirm sheet, deletes your
  Friends account and everything on it from the service in one step, and the
  drafts on your computer, with their pictures and video. Friends is off after.
  `mewla logout -delete-account` does the same first. A deleted account can
  remain in a service backup for up to 30 days.
- **Where it runs.** On the same account service as remote access, in its own
  database. It sees the IP address of connections only as the account service
  does (see the privacy policy).

## Revoke a device

**Remove computer**, in a computer's details in the app (tap the cat, then the
computer's name or ⓘ), asks that computer to revoke the phone's key when it can
reach it within a few seconds, then forgets it on the phone. To revoke a
device's key yourself, on the computer:

```sh
mewla devices list
mewla devices revoke -id <device-id>
```

Revocation closes the device's live connections immediately and rejects its
later requests.

## Report a vulnerability

Report security issues privately to the Mewla maintainers, as described in the
[security policy](https://github.com/daoleno/mewla/security/policy). Please
include:

- the affected part (daemon, app, pairing, upload, executor launch);
- steps to reproduce;
- the impact (for example authentication bypass, code execution on the host,
  data exposure).

Do not open a public issue for an unpatched flaw that can be exploited
remotely. Security fixes target the latest stable release and the main branch.

## Direct connections from a phone on Mewla Relay

A phone paired through Mewla Relay may reach the computer on its Wi-Fi/LAN or
Tailscale address instead of the relay. It only dials private-network IP
addresses the computer itself announced over an authenticated connection, never
a name. The stream is TLS pinned to the computer's transport key, and before
the device signature is sent the computer must answer a fresh 16-byte challenge
from the phone, signed with its daemon key under a purpose used for nothing
else. A recorded answer, another computer's key, a computer that predates the
challenge, or a stale address now held by another device all fail and fall back
to the relay. The connection is as private as the relay one, since the relay
only carries the same pinned TLS stream.


### The private-network listener

So that a phone on Mewla Relay can go direct without any setting, a daemon that listens
only on loopback also listens, on the same port, on this computer's private
IPv4 addresses (the usual home and office ranges, and the carrier-grade NAT range Tailscale uses), and follows those
addresses as they change. That is new exposure on the local network, kept small:

- **TLS only.** The listener presents the daemon's transport identity and
  answers plain HTTP with a bare 400. Nothing is served in the clear.
- **Only device-signed and proof routes.** `/ws`, `/health`, `/auth-check` and
  the other routes that check a paired device's signature are served. Pairing
  (`/pair`, `/p/…`), join requests (`/enrollment/…`), the OAuth return and the
  web UI are not: they answer 404 there, so the pairing boundary is unchanged
  and no one can start a pairing or join request from the network.
- **Same authentication.** A request still needs a valid device signature; a
  phone only sends one after the listener proves the daemon key.
- **Not learned as an address.** A request that arrives on a private address
  is never added to the address book as an HTTPS entry point.
- **What a neighbour on the network can still do:** open TCP connections and
  TLS handshakes, read `/health` (the daemon id and public key, which are not
  secret), and get 401 on everything else. Handshakes and header reads have
  timeouts, but there is no per-address rate limit; if you don't want any of
  this on a network, the firewall is the control.
- On macOS the first listen on a network interface can raise the system's
  incoming-connections prompt for the app.
