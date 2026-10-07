# Troubleshooting

Start with the host check on the computer that runs Mewla:

```sh
mewla doctor
```

It checks `tmux`, the state directory, the listen port, your agent CLIs and the
network, and says what to fix.

## The daemon will not start

- **`mewla: command not found`**: the install directory is not on `PATH`. Open a
  new terminal, or run the `export PATH=...` line the installer printed.
- **Address already in use**: another process holds `127.0.0.1:9876`. Stop it,
  or choose another address with `-addr`.
- **State directory locked**: another `mewla` already uses the same state
  directory, perhaps as a service from `mewla boot install`. Check with
  `mewla boot status`. Only one daemon can own a state directory.
- **State is still in `~/.zen`**: Mewla moves `~/.zen` to `~/.mewla` on its
  first run, but waits while a daemon from before the rename still uses
  `~/.zen`. Stop that daemon, then run `mewla state migrate`. See
  [Coming from Zen](install-daemon.md#coming-from-zen).
- **Permission errors on `~/.mewla`**: the state directory must belong to your
  user. Do not run `mewla` as root.
- **`-advertise-url` is unknown**: that flag no longer exists. Start `mewla`, then
  run `mewla pair <origin>` with the address the phone should use.

## The phone cannot connect

1. On the phone, open `/health` at the exact address you pair with, for example
   `https://mewla.example.com/health`. If it does not load, the problem is the
   network, not Mewla.
2. On the same Wi-Fi, check the computer's firewall and whether the network
   isolates clients from each other. Pair with the private address Mewla printed,
   never `0.0.0.0`.
3. Behind a tunnel or proxy, forward the whole origin with WebSocket upgrades,
   not only `/ws`. Path prefixes are not supported.
4. Generate a fresh code with `mewla pair <origin>`; codes expire after 15
   minutes and work once. Import it in **Settings > Pair a server**.
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
- Builds of the app you made yourself need your own Expo project for push.

## A plugin says "Not yet available"

That service's sign-in needs Mewla's own registration with the provider, which
has not shipped yet. See [Plugins](plugins.md#sign-in-availability).

## Codex or Claude fail outside Mewla while the daemon is stopped

Mewla points both CLIs at its local gateway, which only runs with the daemon.
Start `mewla`, or remove the gateway settings as described in
[Providers and usage](providers-and-usage.md#how-routing-works).

## macOS blocks the binary

Confirm it came from an official Mewla release, then run
`xattr -d com.apple.quarantine "$(command -v mewla)"`.
