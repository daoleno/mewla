# Troubleshooting

Start with the host check on the computer that runs Mewla:

```sh
mewla doctor
```

It checks `tmux`, the state directory, the listen port, your agent CLIs and the
network, and says what to fix.

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
close is logged as `websocket: client connection lost ... device=<id>`. To
check a stuck screen afterwards:

```sh
grep -E 'client diagnostic|client connection lost' ~/.mewla/logs/daemon.log | tail -n 30
```

A device id is in `mewla devices list`. Each event is reported at most once
every 10 seconds per device.

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
   particular address); codes expire after 15 minutes and work once. Import it
   in **Settings > Pair a computer**.
5. With a custom `-state-dir`, pass the same value to `mewla` and `mewla pair`.

## It paired, but no Sessions appear or agents fail to start

- Install and sign in to at least one agent CLI on the computer, as the same
  user that runs `mewla`. One is enough.
- Check that `tmux` works for that user.
- If you have `~/.mewla/executors.toml`, check its commands; a typo there makes
  launches fail. Restart `mewla` after editing it.

## Chat is empty

Chat comes from the agent's own history files. A brand-new Session shows
little until the agent writes its first messages. Plain shells and other
commands have Terminal only.

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
  [Notifications](notifications.md#push-notifications).

## A plugin says "Not yet available"

That service's sign-in needs Mewla's own registration with the provider, which
has not shipped yet. See [Plugins](plugins.md#sign-in-availability).

## Codex or Claude fail outside Mewla while the daemon is stopped

Mewla points both CLIs at its local gateway, which only runs with the daemon.
Start `mewla`, or remove the gateway settings as described in
[Model Providers](executors.md#model-providers).

## macOS blocks the binary

Confirm it came from an official Mewla release, then run
`xattr -d com.apple.quarantine "$(command -v mewla)"`.
