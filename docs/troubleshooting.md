# Troubleshooting

Start with the host check on the computer that runs Zen:

```sh
zen doctor
```

It checks `tmux`, the state directory, the listen port, your agent CLIs and the
network, and says what to fix.

## The daemon will not start

- **`zen: command not found`**: the install directory is not on `PATH`. Open a
  new terminal, or run the `export PATH=...` line the installer printed.
- **Address already in use**: another process holds `127.0.0.1:9876`. Stop it,
  or choose another address with `-addr`.
- **State directory locked**: another `zen` already uses the same state
  directory, perhaps as a service from `zen boot install`. Check with
  `zen boot status`. Only one daemon can own a state directory.
- **Permission errors on `~/.zen`**: the state directory must belong to your
  user. Do not run `zen` as root.
- **`-advertise-url` is unknown**: that flag no longer exists. Start `zen`, then
  run `zen pair <origin>` with the address the phone should use.

## The phone cannot connect

1. On the phone, open `/health` at the exact address you pair with, for example
   `https://zen.example.com/health`. If it does not load, the problem is the
   network, not Zen.
2. On the same Wi-Fi, check the computer's firewall and whether the network
   isolates clients from each other. Pair with the private address Zen printed,
   never `0.0.0.0`.
3. Behind a tunnel or proxy, forward the whole origin with WebSocket upgrades,
   not only `/ws`. Path prefixes are not supported.
4. Generate a fresh code with `zen pair <origin>`; codes expire after 15
   minutes and work once. Import it in **Settings > Pair a server**.
5. With a custom `-state-dir`, pass the same value to `zen` and `zen pair`.

## It paired, but no Sessions appear or agents fail to start

- Install and sign in to at least one agent CLI on the computer, as the same
  user that runs `zen`. One is enough.
- Check that `tmux` works for that user.
- If you have `~/.zen/executors.toml`, check its commands; a typo there makes
  launches fail. Restart `zen` after editing it.

## Chat is empty

Chat comes from the agent's own history files. A brand-new Session shows
little until the agent writes its first messages. Plain shells and other
commands have Terminal only.

## The terminal is unavailable

The release APK includes the native terminal. If you built the app yourself,
build the native terminal library first: `bun run native:build` for Android,
`bun run native:build:ios` for iOS.

## A Worker shows "unknown"

`unknown` means Zen has no reliable evidence about the Worker's current turn.
It does not mean the agent stopped. Open the Session to see what it is doing;
Brain decides what to do with its Work.

## No push notifications

- Allow notifications for Zen in the phone's settings.
- You are not notified about the Session you are looking at.
- Each push is a single attempt; a missed one does not come back. The state is
  in the app when you open it.
- Builds of the app you made yourself need your own Expo project for push.

## A plugin says "Not yet available"

That service's sign-in needs Zen's own registration with the provider, which
has not shipped yet. See [Plugins](plugins.md#sign-in-availability).

## Codex or Claude fail outside Zen while the daemon is stopped

Zen points both CLIs at its local gateway, which only runs with the daemon.
Start `zen`, or remove the gateway settings as described in
[Providers and usage](providers-and-usage.md#how-routing-works).

## macOS blocks the binary

Confirm it came from an official Zen release, then run
`xattr -d com.apple.quarantine "$(command -v zen)"`.
