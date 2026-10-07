# Install

Zen has two parts: the daemon on your computer and the app on your phone.
Install the daemon first; the app is useless without it.

## What you need

- Linux `amd64`/`arm64`, WSL2, or an Apple Silicon Mac
- `curl`, `tar`, and `sha256sum` (Linux) or `shasum` (macOS)
- `tmux` on `PATH`
- At least one agent CLI on `PATH` and already signed in: `claude`, `codex`,
  `cursor-agent`, `grok`, `pi` or `opencode`. One is enough.

## Install the daemon

```sh
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | sh
```

The installer picks the newest release for your platform, downloads the archive
and its `SHA256SUMS`, verifies the checksum, and installs `zen` into a
user-owned directory on your `PATH` (or `~/.local/bin`). It never uses `sudo`,
installs packages, installs agent CLIs, signs in anywhere or sends telemetry.
When it finishes, it runs `zen doctor`.

If `zen` is already installed, the same command runs `zen update` instead,
unless you set `ZEN_VERSION` or `ZEN_INSTALL_DIR`.

Options, set as environment variables on `sh`:

| Variable | Effect |
| --- | --- |
| `ZEN_VERSION=v0.1.15` | Install this exact release tag instead of the newest |
| `ZEN_INSTALL_DIR="$HOME/bin"` | Install into this user-owned directory |
| `ZEN_NO_PATH_UPDATE=1` | Do not add the install directory to your shell profile |
| `ZEN_DRY_RUN=1` | Print the platform, version and destination; change nothing |

```sh
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | ZEN_INSTALL_DIR="$HOME/bin" sh
```

If the installer added a directory to your shell profile, open a new terminal
or run the `export PATH=...` line it printed.

### Manual download

Download the archive for your host and `SHA256SUMS` from the
[GitHub Releases](https://github.com/daoleno/mewla/releases), then verify and install:

```sh
# Linux (use zen-linux-arm64.tar.gz on ARM)
grep 'zen-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -
tar -xzf zen-linux-amd64.tar.gz

# macOS
grep 'zen-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c -
tar -xzf zen-darwin-arm64.tar.gz

mkdir -p ~/.local/bin
install -m 755 zen ~/.local/bin/zen
~/.local/bin/zen doctor
```

On macOS, install `tmux` with `brew install tmux`. If macOS blocks the binary,
confirm it came from an official Zen release, then run
`xattr -d com.apple.quarantine ~/.local/bin/zen`.

## Supported platforms

| Host | Archive | Status |
| --- | --- | --- |
| Linux `amd64` / `x86_64` | `zen-linux-amd64.tar.gz` | Supported |
| Linux `arm64` / `aarch64` | `zen-linux-arm64.tar.gz` | Supported |
| WSL2 on either architecture | The matching Linux archive | Supported |
| Apple Silicon macOS | `zen-darwin-arm64.tar.gz` | Supported |
| Intel macOS | None | Not supported |
| Native Windows | None | Not supported; use WSL2 |

The installer stops on an unsupported platform before downloading anything.

## Check the host

```sh
zen doctor          # tmux, state directory, listen port, agent CLIs
zen doctor --json
```

`zen doctor` exits nonzero when the host is not ready and says why. It never
installs packages or prints credentials.

To write an executor configuration interactively, run `zen setup`. See
[Agents and executors](executors.md#configure-executors).

## Update

```sh
zen update --check  # is there a newer release?
zen update          # verify and install it
```

`zen update` checks the release's signed manifest and the archive checksum
before it replaces the binary. It does not restart a running daemon; stop and
start `zen` when convenient.

## Keep it running

`zen` is the whole runtime. Run it in a terminal or a `tmux` window, or, on
Linux with systemd, let Zen install a user service for you:

```sh
zen boot install          # this binary, default state and address
zen boot install -lan     # same, listening on the private network
zen boot install -dry-run # print the unit without installing it
zen boot status
zen boot uninstall
```

`zen boot install` refuses to run as root and refuses to start a second daemon
on a state directory that is already in use; stop the running `zen` first.
Restarting the service never stops your `tmux` sessions or agents. To start it
before you log in, enable lingering with `loginctl enable-linger` (the command
tells you when this is needed).

Do not run the daemon as root.

## Defaults

| Setting | Value |
| --- | --- |
| Listen address | `127.0.0.1:9876` (`zen --lan` uses `0.0.0.0:9876`) |
| State directory | `~/.zen` |
| Executor configuration | `~/.zen/executors.toml` (optional) |
| Brain workspace | `~/.zen/brain` |

To use another state directory, pass the same `-state-dir` to every command:

```sh
zen -state-dir /path/to/state
zen pair -state-dir /path/to/state https://zen.example.com
```

## Install the app

### Android

Download `zen-android-arm64-v<version>.apk` and `SHA256SUMS` from the
[GitHub Releases](https://github.com/daoleno/mewla/releases) and verify the APK:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

The APK supports 64-bit ARM phones (`arm64-v8a`) only. Android asks you to
allow installs from your browser or file manager, and Play Protect may warn
because Zen is not on the Play Store. The official signing certificate's SHA-256
fingerprint is:

```text
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:
4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

### iOS

A [TestFlight preview](https://testflight.apple.com/join/rTKCDzMt) is set up
but still awaiting Apple's beta review, so the link may not let you install yet.
Until then, build the app from source on an Apple Silicon Mac.

#### Build the iOS app from source

You need full Xcode, an iOS Simulator runtime or a signed device target,
CocoaPods, Bun, and network access for the first native build. Installing on a
physical iPhone also needs an Apple development team.

Clone the source, then from the repository root:

```sh
git clone https://github.com/daoleno/mewla.git && cd zen
bun install
bun run native:build:ios     # builds the pinned terminal library
bun run native:verify:ios
cd app
bunx expo prebuild --platform ios --clean
cd ios && pod install && cd ..
bun run ios
```

You can also open `app/ios/Zen.xcworkspace` in Xcode after `pod install`. The
app targets iOS 16.4 or newer and asks for local-network access (to reach your
daemon) and camera access (to scan pairing codes).

### Web UI

The daemon also serves the app as a web page for a browser on the same
computer or behind HTTPS. See [Pair a browser](connect-and-pair.md#pair-a-browser).

## Build the daemon from source

With the Go version named in `daemon/go.mod` and Bun installed, from the
repository root:

```sh
bun install
bun run daemon:build   # writes bin/zen
./bin/zen doctor
```

A source build serves a placeholder instead of the web UI unless you first run
`bun run web:build`.

## Uninstall

1. Run `zen boot uninstall` if you installed the service, and stop `zen`.
2. Delete the binary (`command -v zen` shows where it is).
3. Optionally delete `~/.zen`. It holds the daemon's identity, paired devices,
   Work and Brain data; deleting it unpairs every phone.
