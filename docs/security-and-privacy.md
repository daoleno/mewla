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

There is no long-lived shared secret for normal traffic, and no separate admin
role: every paired phone or browser can list and revoke devices, including itself.

## What a paired device can do

A paired device can open any Session's live terminal, so it has the same power
over your computer as those Sessions. File preview is read-only, but it is not
a sandbox: it can open files anywhere the daemon's user can read. **Pair only
devices you trust with a terminal on that computer.**

[Screen](screen.md) lets a paired device see and control the computer's
screen, but only after someone at the computer allows that device. The daemon
captures the screen itself and serves it over the device's signed connection;
no screen-sharing port is opened. `mewla screen disable` at the computer closes
every viewer and forgets every allowed device.

## Your other computers

A computer you connect under **Working together** (or with
`mewla computers add`) gets its own device key of a separate kind. That key
opens only the computer channel: it can send messages to this computer's
Brain and receive the answers, and nothing else. No terminal, files, Screen
or device management. Each side pins the other's key, and plain HTTP outside
Tailscale or loopback is refused, because the channel's messages are not
signed one by one after it opens.

A message from another computer reaches Brain marked as relayed by your other
Brain, with your own words quoted separately. Brain does not treat it as your
consent: before payments, messages to other people, deletion, account changes
or installing software it asks you through the Brain you are talking to.
`mewla computers remove` or `mewla devices revoke` ends the access at once.
See [Your other computers](computers.md).

## Agents on the screen

On a Mac with Screen permissions, agents Mewla starts can see the screen and
use the keyboard and pointer while **Agents can use this computer** is on. The
daemon acts for them, so their own processes never hold the permissions. Each
request must name a running Mewla Worker, and one agent uses the screen at a
time. The request travels over the daemon's private socket, which any process
of your user on that computer can reach, as with the rest of `mewla`. Agents
don't need a phone's Screen approval: the switch is the approval. A person
viewing the screen always takes over from an agent, someone using the Mac's own
keyboard or trackpad pauses it, agents can't use a locked Mac, the app's switch
stops agents at once (`mewla screen disable` and `mewla agent-use off` within
seconds), and each action is logged without what was typed. See
[Screen](screen.md#agents-on-the-screen).

## Agents in your browser

When **Agents can use this computer** is on and the Playwright extension is in
Chrome, agents Mewla starts on that computer can use your Chrome with your
logins, through the extension. Each works in a tab group of its own and sees
only that group. The switch turns it off for new agents at once.

The extension's key lets anything that has it attach to your Chrome without
asking. Mewla reads it from the extension's storage in your Chrome profile (or
keeps the one you pasted, in a file only your user can read) and hands it to
the browser tools in their environment only: never on a command line, in a log
or in an agent's prompt. Regenerate it in the extension to cut off every copy.
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
  pair.

See [Connect and pair](connect-and-pair.md) for each route.

## Agents and model providers

Agents run with your user's permissions, and some run without approval prompts.
That is a choice about your computer, not something Mewla's network layer can
contain. Read [Permission bypass risks](executors.md#permission-bypass-risks).

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
| `uploads/` | Files sent from the app or Telegram: at most 2 GiB per file and 8 GiB in total, deleted after seven days |
| `work/`, `brain/` | Work records and Brain's workspace |
| `calendar/` | Calendar items |
| `telegram/` | Telegram binding and delivery state; the bot token is in its own private file |
| `worktrees/`, `t/` | Worker scratch space and temporary files |

Plugin and Model Provider credentials are stored in private files in the state
directory, never shown back in the app.

Treat the state directory like your SSH keys: keep it private (`0700`), never
commit it, and never share a pairing link.

## Data on your phone

The device's private key is kept in the platform's secure storage (Keychain on
iOS, Keystore-backed storage on Android). A paired browser keeps its key in the
page's local storage, which is weaker; pair only browser profiles you control.

## Outbound connections

The daemon connects out only for features you use:

| Destination | When |
| --- | --- |
| The release server | `mewla update`, and an occasional update hint when you start `mewla` in a terminal |
| `models.dev` | Model names and reference prices for Stats and Model Providers |
| Expo push service | Sending a push notification to your phone |
| The model provider you selected | Requests from Codex and Claude Code through the local gateway |
| Connected plugin services | Calls Brain or Workers make |
| Telegram Bot API | When Telegram is connected |
| Cloudflare | When you start a Quick Tunnel |

Mewla sends no telemetry.

## Remote access account

`mewla login` is the only Mewla service that keeps data about you: your
GitHub id and username, and each signed-in computer's address, name and public
key. `mewla logout -delete-account` deletes the account and every computer on
it. The [privacy policy](https://mewla.ai/privacy/) lists everything the
service and its relay see and keep.

## Revoke a device

Removing a server in the app asks that computer to revoke the phone's key when
it can reach it within a few seconds, then forgets it on the phone. To revoke a
device's key yourself, on the computer:

```sh
mewla devices list
mewla devices revoke -id <device-id>
```

Revocation closes the device's live connections immediately and rejects its
later requests.

## Diagrams in Markdown

Mermaid diagrams in agent output are treated as untrusted. They render in an
isolated view that cannot load network resources, open windows or run click
handlers; unsupported or oversized diagrams are shown as source.

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
