# Troubleshooting

Start with the host check on the computer that runs Mewla:

```sh
mewla doctor
```

It checks the platform, the state directory, the listen port, your agent CLIs
and the network, and says what to fix.

## What the app tells you

The state control next to **Brain** says how the computer is, and the tray
(tap it) says why and offers the fix:

| State control | What it means | What to do |
| --- | --- | --- |
| **Connecting…** | The app is reaching the computer. Messages you send wait and go when it connects | Wait |
| **Offline** | The app can't reach the computer. The tray names the problem, such as **Relay unreachable**, or **It may be asleep** | **Try again** in the tray, or **Details** for the computer page. If it stays offline, see [The phone cannot connect](#the-phone-cannot-connect) |
| **Pair again** | This computer no longer trusts the phone | Tap it for the pairing sheet; run `mewla pair` on the computer and scan the new code |
| **Starting…** | Brain is starting | Wait |
| **Can't start** | Brain can't start; the tray says why | **Use …** moves Brain to the ready agent it names; **Switch executor** picks another. See [It paired, but no Sessions appear or agents fail to start](#it-paired-but-no-sessions-appear-or-agents-fail-to-start) |

On a pushed page, a dot on Back means the computer is connecting, offline or
must be paired again.

The computer page (tap the cat) lists what **Needs fixing**, each with its fix:
**Relay unreachable** › **Try again**, **Device not paired** › **Pair**,
**Claude is not signed in** › **Switch**, **Chrome extension missing** ›
**Fix**, **Update required** › **Update**. A dot on the cat means this
computer must be paired again or needs a required update, or another computer
has requests waiting for you.

## Reading the daemon's terminal output

After the startup summary (version, listen address, data directory, pairing
addresses), a healthy daemon prints nothing. Every later line reports a
problem: a failure, a retry, a degraded state or a rejected request. Each line
names its subsystem and the ids involved, such as `worker=`, `request=` or
`remote=`, so you can paste it into an issue or hand it to an agent.

To also see routine events (clients connecting, stats refreshes, uploads,
skipped pushes), start the daemon with verbose logging:

```sh
MEWLA_LOG_VERBOSE=1 mewla
```

Every line the daemon logs is also appended to `~/.mewla/logs/daemon.log`
(under `--state-dir` when you set one), so it is still there after the terminal is
closed or the daemon restarts as a login service. The file rotates at 10 MB
into `daemon.log.1`, keeping at most two files. Startup details and pairing
links go only to the terminal, never to the file.

```sh
tail -n 50 ~/.mewla/logs/daemon.log
```

### When the phone's screen stops updating

The app reports a few moments only it can see as one line each in the daemon
log: `client diagnostic device=<id> event=<name> ...`. `stream_gap` means a live
conversation skipped a revision and resubscribed, `snapshot_wait` means no
snapshot came back within 8 seconds of subscribing, `send_unsettled` means a
send was not confirmed, and `pending_unreconciled` means a confirmed send
still had no row 45 seconds later. A connection that dropped without a clean
close is logged as `websocket: client connection lost ... device=<id> cause=<who>`,
and so is any connection that was sent more than 1 MiB, even when it ended cleanly.
The cause says which end hung up:

- `peer-closed`: the phone, or something between it and the daemon (a tunnel,
  the phone's operating system closing the app's sockets), closed the
  connection without a goodbye.
- `client-silent`: the phone did not answer the daemon's pings for 60 seconds,
  as when the app is frozen in the background.
- `daemon-closed(...)`: the daemon ended it, and the brackets say why (a ping
  that could not be sent, or a client that fell too far behind).

The rest of the line shows the link: bytes sent, bytes still queued, the last
send's size, age and duration, and how many pings the phone answered
(`pongs=answered/sent`) and when. `types=` names the message types that filled
the link, heaviest first, as `type:bytes/count`; `other` is the rest, and the
byte counts are before compression (`deflate=true` says the phone offered
compression, so JSON text is smaller on the wire). A phone's own report of how its socket ended
is `event=ws_closed`; it carries the platform's error text (`error=`) and the
last frame it received (`lastFrame=type/size`). To check a stuck screen
afterwards:

```sh
grep -E 'client diagnostic|client connection lost' ~/.mewla/logs/daemon.log | tail -n 30
```

A device id is in `mewla devices list`. Each event is reported at most once
every 10 seconds per device.

When a phone or browser is turned away with `401` (or `404` on the cat's
files), the daemon says why in one line,
`auth: refused <method> <path> purpose=<p> reason=<why> device=<id> remote=<addr>`,
at most once every 30 seconds per reason and device (the line says how many
more were held back). The reasons are `malformed-header`, `wrong-daemon`,
`clock-skew` (the phone's clock is more than 5 minutes from the computer's;
the line says by how much), `replay` (the same signed request arrived twice;
the line says how long after the first, which separates a request resent after
a lost response from a captured one), `unknown-device` (revoked or never
paired), `wrong-purpose` and `bad-signature`. The signature, nonce and
Authorization header are never logged.

## The app froze or closed

On Android, if the app stops responding for 10 seconds while it is open, it
reopens itself and reconnects; after a few reopens in a row it stays as it is
instead of looping. On the next start a sheet shows what happened, such as
**Stopped responding** or a crash, with **Copy** for the full report, and the
computer's log gets a one-line summary. Include the copied report when you
[open an issue](https://github.com/daoleno/mewla/issues).

## The daemon will not start

- **`mewla: command not found`**: the install directory is not on `PATH`. Open a
  new terminal, or run the `export PATH=...` line the installer printed.
- **Address already in use**: another process holds `127.0.0.1:9876`. Stop it,
  or choose another address with `-addr`.
- **State directory locked**: another `mewla` already uses the same state
  directory, perhaps as a service from `mewla boot install`. Check with
  `mewla boot status`. Only one daemon can own a state directory.
- **Permission errors on `~/.mewla`**: the state directory must belong to your
  user. Do not run `mewla` as root.

## The phone cannot connect

1. On the phone, open `/health` at the exact address you pair with, for example
   `https://mewla.example.com/health`. If it does not load, the problem is the
   network, not Mewla.
2. On the same Wi-Fi, check the computer's firewall and whether the network
   isolates clients from each other. Start with `mewla --lan`; `mewla address list`
   shows the Wi-Fi/LAN address the phone should reach.
3. Behind a tunnel or proxy, forward the whole origin with WebSocket upgrades,
   not only `/ws`. Path prefixes are not supported.
4. Generate a fresh code with `mewla pair` (or `mewla pair <address>` for one
   particular address); codes expire after 15 minutes and work once. Scan it
   from the computer page's **Pair a computer** (tap the cat).
5. With a custom `-state-dir`, pass the same value to `mewla` and `mewla pair`.

## It paired, but no Sessions appear or agents fail to start

- Install and sign in to at least one agent CLI on the computer, as the same
  user that runs `mewla`. One is enough. Until then the state control reads
  **Can't start**, and the tray says which step is missing (`mewla deps`
  installs one on Linux and macOS, `claude auth login` or `codex login` signs
  in). Brain starts by itself once it is done; no restart needed.
- `mewla doctor` also asks the running Mewla and warns when an agent is ready in
  your terminal but not for Mewla: a sign-in that lives in your shell's
  environment (an API key or token you export) is invisible to Mewla when it
  starts at login. Sign in once with the agent's own login so it is kept for
  every program, or stop Mewla and start it again from that terminal. It also
  warns the other way: when the running Mewla has an agent signed in that your
  terminal does not see (a different PATH or environment), Brain uses the running
  Mewla's view, not the terminal's. `mewla doctor` looks for agents where Mewla
  does: on PATH, then `~/.local/bin` and the other native-installer folders.
- If the tray says Brain's session "is open but not ready", the agent is
  waiting at a sign-in or first-run question in its terminal. Run that agent
  once on the computer, or answer it in Brain's terminal (⋯ › **Open
  terminal**), and Brain continues.
- If you have `~/.mewla/executors.toml`, check its commands; a typo there makes
  launches fail. Restart `mewla` after editing it.

## Chat is empty

Chat comes from the agent's own history files. A brand-new Session shows
little until the agent writes its first messages. Plain shells and other
commands have Terminal only. When Brain's chat reads **Chat isn't available**
for its agent, or **Couldn't load the chat**, **Open terminal** shows the same
Session as a terminal; **Try again** reloads the chat.

## The terminal is unavailable

The release APK includes the native terminal. If you built the app yourself,
build the native terminal library first: `bun run native:build` for Android,
`bun run native:build:ios` for iOS.

## A Worker shows "unknown"

`unknown` means Mewla has no reliable evidence about the Worker's current turn.
It does not mean the agent stopped. Open the Session to see what it is doing;
Brain decides what to do with its Work.

## No push notifications

- Allow notifications for Mewla in the phone's settings.
- You are not notified about the Session you are looking at.
- Each push is a single attempt; a missed one does not come back. The state is
  in the app when you open it.
- The daemon logs a failed or undelivered push (`push: ... failed`). Run it
  with `MEWLA_LOG_VERBOSE=1` to see skipped ones too.
- Remote push on iOS is not yet available in official builds.
- A fork's own build needs its own Expo project and Firebase app; see
  [Notifications](notifications.md).

## An extension says "Not yet available"

That service's sign-in needs Mewla's own registration with the provider, which
has not shipped yet. See [Extensions](extensions.md).

## Codex or Claude fail outside Mewla while the daemon is stopped

Mewla points both CLIs at its local gateway, which only runs with the daemon.
Start `mewla`, or remove the gateway settings as described in
[Model Providers](executors.md#model-providers).

## macOS blocks the binary

Confirm it came from an official Mewla release, then run
`xattr -d com.apple.quarantine "$(command -v mewla)"`.
