# Security and privacy

Zen is self-hosted. Your repositories, credentials and agent state stay on your
computer, and Zen operates no cloud service that stores your transcripts.

## Trust model

Zen separates three things:

1. **Reachability**: how the phone reaches the daemon (LAN, Tailscale, a
   tunnel or a reverse proxy). Reaching the daemon grants nothing by itself.
2. **Daemon identity**: a persistent Ed25519 key in the state directory.
3. **Device authorization**: each phone or browser enrolls its own Ed25519 key
   once, with a pairing code that expires after 15 minutes and works once.
   Every later request is signed by the device and checked by the daemon.

There is no long-lived shared secret for normal traffic, and no separate admin
role: every paired device can list and revoke devices, including itself.

## What a paired device can do

A paired device can open any Session's live terminal, so it has the same power
over your computer as those Sessions. File preview is read-only, but it is not
a sandbox: it can open files anywhere the daemon's user can read. **Pair only
devices you trust with a terminal on that computer.**

## What to expose

- The daemon listens on `127.0.0.1:9876` by default, reachable only from the
  same computer.
- `zen --lan` serves plain HTTP on every IPv4 interface. Use it only on a
  network you trust, and restrict port `9876` with a firewall.
- Through Tailscale, traffic stays inside your tailnet and its access rules.
- An HTTPS tunnel or reverse proxy makes the daemon reachable from the
  internet according to that service's settings. `/health` answers without
  authentication; every other route needs a pairing code or a device signature.
- The web UI is served only to the same computer, or on HTTPS origins you allow
  with `-web-origin`. Loading the page grants nothing; the browser must still
  pair.

See [Connect and pair](connect-and-pair.md) for each route.

## Agents and model providers

Agents run with your user's permissions, and some run without approval prompts.
That is a choice about your computer, not something Zen's network layer can
contain. Read [Permission bypass risks](executors.md#permission-bypass-risks).

Model providers see whatever the agents send them. Zen reads agent transcripts
from each agent's own home directory to show Chat and Stats.

## Data on your computer

All paths are under the state directory, `~/.zen` by default.

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
| The release server | `zen update`, and an occasional update hint when you start `zen` in a terminal |
| `models.dev` | Model names and reference prices for Stats and Model Providers |
| Expo push service | Sending a push notification to your phone |
| The model provider you selected | Requests from Codex and Claude Code through the local gateway |
| Connected plugin services | Calls Brain or Workers make |
| Telegram Bot API | When Telegram is connected |
| Cloudflare | When you start a Quick Tunnel |

Zen sends no telemetry.

## Revoke a device

Removing a server in the app only forgets it on that phone. To revoke a device's
key, on the computer:

```sh
zen devices list
zen devices revoke -id <device-id>
```

Revocation closes the device's live connections immediately and rejects its
later requests.

## Diagrams in Markdown

Mermaid diagrams in agent output are treated as untrusted. They render in an
isolated view that cannot load network resources, open windows or run click
handlers; unsupported or oversized diagrams are shown as source.

## Report a vulnerability

Report security issues privately to the Zen maintainers at
{{ZEN_SECURITY_CONTACT}}. Please include:

- the affected part (daemon, app, pairing, upload, executor launch);
- steps to reproduce;
- the impact (for example authentication bypass, code execution on the host,
  data exposure).

Do not open a public issue for an unpatched flaw that can be exploited
remotely. Security fixes target the latest stable release and the main branch.
