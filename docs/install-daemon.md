# Install and update

Everything [Get started](get-started.md) skips: platforms, manual downloads,
running as a service, updates, the app builds and uninstalling.

## What you need

- Linux `amd64`/`arm64`, WSL2, or an Apple Silicon Mac
- `curl`, `tar`, and `sha256sum` (Linux) or `shasum` (macOS)
- `tmux` on `PATH`; the installer and `mewla` offer to install it
- At least one agent CLI on `PATH` and already signed in: `claude`, `codex`,
  `cursor-agent`, `grok`, `pi` or `opencode` (OpenCode 2; `mewla doctor` names
  the upgrade command when it finds OpenCode 1). One is enough; `mewla` offers
  Claude Code and Codex when none is ready, and asks which one Brain uses
  when several are.

## Install the daemon

```sh
curl -fsSL https://mewla.ai/install.sh | sh
```

`https://mewla.ai/install.sh` serves the same file as
`https://raw.githubusercontent.com/daoleno/mewla/main/install.sh`, which keeps working.

The installer follows GitHub's `releases/latest` redirect to the newest stable
release (no GitHub API call, so API rate limits don't apply), downloads the
archive for your platform and its `SHA256SUMS`, verifies the checksum, and
installs `mewla` into a user-owned directory on your `PATH` (or
`~/.local/bin`). It sends no telemetry and installs `mewla` without `sudo`.

Then it runs `mewla deps -only tmux`. When `tmux` is missing, that prints the
one official command for this system and, in a terminal, asks
`Install tmux now? [Y/n]`. It runs exactly that command; `sudo` asks for your
password. Without a terminal it only prints the command:

| System | Command |
| --- | --- |
| Debian, Ubuntu, WSL | `sudo apt update && sudo apt install -y tmux` |
| Fedora, RHEL family | `sudo dnf install -y tmux` (`yum` without `dnf`) |
| Arch family | `sudo pacman -S --needed --noconfirm tmux` |
| openSUSE | `sudo zypper install -y tmux` |
| Alpine | `sudo apk add tmux` |
| macOS with Homebrew | `brew install tmux` |
| macOS without Homebrew | The [Homebrew installer](https://brew.sh), then `/opt/homebrew/bin/brew install tmux` |
| Anything else | A link to the [tmux install page](https://github.com/tmux/tmux/wiki/Installing) |

The last line is the command that starts Mewla: `mewla`, or its full path
when the current shell cannot find it yet. New terminals find `mewla` once the
installer has added its directory to your shell profile.

If `mewla` is already installed, the same command runs `mewla update` instead,
unless you set `MEWLA_VERSION` or `MEWLA_INSTALL_DIR`.

Options, set as environment variables on `sh`:

| Variable | Effect |
| --- | --- |
| `MEWLA_VERSION=v0.2.91` | Install this exact release tag, beta included, instead of the newest stable |
| `MEWLA_INSTALL_DIR="$HOME/bin"` | Install into this user-owned directory |
| `MEWLA_NO_PATH_UPDATE=1` | Do not add the install directory to your shell profile |
| `MEWLA_DRY_RUN=1` | Print the platform, version and destination; change nothing |

```sh
curl -fsSL https://mewla.ai/install.sh | MEWLA_INSTALL_DIR="$HOME/bin" sh
```

### Manual download

Download the archive for your host and `SHA256SUMS` from the
[GitHub Releases](https://github.com/daoleno/mewla/releases), then verify and install:

```sh
# Linux (use mewla-linux-arm64.tar.gz on ARM)
grep 'mewla-linux-amd64.tar.gz$' SHA256SUMS | sha256sum -c -
tar -xzf mewla-linux-amd64.tar.gz

# macOS
grep 'mewla-darwin-arm64.tar.gz$' SHA256SUMS | shasum -a 256 -c -
tar -xzf mewla-darwin-arm64.tar.gz

mkdir -p ~/.local/bin
install -m 755 mewla ~/.local/bin/mewla
~/.local/bin/mewla doctor
```

On macOS, install `tmux` with `brew install tmux`. If macOS blocks the binary,
confirm it came from an official Mewla release, then run
`xattr -d com.apple.quarantine ~/.local/bin/mewla`.

## Set up a computer over SSH

For a Linux box or Mac mini you reach only through a terminal, one command
installs Mewla, keeps it running and pairs your phone, with no browser on that
computer and nothing to copy between devices:

```sh
curl -fsSL https://mewla.ai/install.sh | sh -s -- --remote
```

After the usual install it runs `mewla setup remote`, which you can also run
yourself any time:

1. **Sign in.** It prints an address and a short code. Open the address on your
   phone or any device, enter the code and sign in. No port is opened on the
   computer and nothing is pasted back.
2. **Run in the background.** It starts Mewla as a user service that starts
   with the computer ([`mewla boot install`](#keep-it-running)). Where there is
   no service manager, such as a container, it starts Mewla detached from your
   SSH session and says that it will not survive a restart.
3. **Pair your phone.** It draws the QR code and prints the same link to paste
   into the app (**Settings > Pair a computer**).
4. **Wait.** It waits up to 15 minutes, until the code expires, for the phone,
   then says so.

If no agent CLI is signed in on that computer yet, it ends by naming the step:
`mewla deps -only agent`. Brain cannot answer until one is.

It is safe to run again: a computer already signed in skips step 1, and a
running Mewla is left alone. With `-lan` it skips the sign-in and pairs over
the computer's Wi-Fi/LAN (plain HTTP, trusted networks only). `-no-wait`
returns after printing the code, and `-address https://…` pairs through an
address you set up yourself. The installer never reads the terminal, so it
works from a script. An older release without `setup remote` falls back to the
usual "Next, start Mewla" line.

## Supported platforms

| Host | Archive | Status |
| --- | --- | --- |
| Linux `amd64` / `x86_64` | `mewla-linux-amd64.tar.gz` | Supported |
| Linux `arm64` / `aarch64` | `mewla-linux-arm64.tar.gz` | Supported |
| WSL2 on either architecture | The matching Linux archive | Supported |
| Apple Silicon macOS | `mewla-darwin-arm64.tar.gz` | Supported |
| Intel macOS | None | Not supported |
| Native Windows | None | Not supported; use WSL2 |

The installer stops on an unsupported platform before downloading anything.

## Check the host

```sh
mewla doctor          # tmux, state directory, listen port, agent CLIs
mewla doctor --json
```

`mewla doctor` exits nonzero when the host is not ready and says why. It never
installs packages or prints credentials.

To write an executor configuration interactively, run `mewla setup`. See
[Agents and models](executors.md#executorstoml).

## Update

```sh
mewla update --check  # is there a newer release?
mewla update          # verify and install it
```

`mewla update` finds the newest stable release the way the install script
does, from GitHub's `releases/latest` redirect, so it needs no GitHub API call
and API rate limits don't apply. Betas are not offered; install one with
`MEWLA_VERSION`. It checks the release's signed manifest and the archive
checksum before it replaces the binary. Run by hand it does not restart a
running daemon; stop and start `mewla` when convenient. On macOS with the
service installed, it also replaces `Mewla.app` and restarts the service,
keeping your address, directories and `PATH`. Your `tmux` sessions and agents
keep running.

### Automatic updates

A released `mewla` on Linux checks every six hours and installs a newer stable
release once no Brain turn or Worker is running. **Settings → your computer**
in the app shows the version the computer runs and, when a newer release
exists, an **Update** button that does the same right away. When the app and
the computer no longer fit together, the same row says so with the same button.

An update never leaves the computer without a working Mewla. The running
process shuts down, then starts the new binary itself and replaces the
installed one only after the new daemon answers on its health endpoint as the
new version. If it exits, hangs or never answers, the old process kills it, puts
the databases back and starts the old version again; that version is not tried
again. The previous binary stays next to the installed one as `mewla.prev`.
A power cut in the middle leaves the old binary in place, and the next start
puts the databases back.

Development builds, betas and macOS (until it is verified) do not update
themselves. `MEWLA_NO_AUTOUPDATE=1` turns automatic updates off. Updating from
a version that predates this (0.2.87 and older) is still the manual
`mewla update`.

## Keep it running

`mewla` is the whole runtime. Run it in a terminal or a `tmux` window, or let
Mewla start it for you: a user service on Linux with systemd, a LaunchAgent on
macOS.

```sh
mewla boot install          # this binary, default state and address
mewla boot install -lan     # same, listening on the private network
mewla boot install -dry-run # print the service without installing it
mewla boot status
mewla boot uninstall
```

The first `mewla` asks `Start Mewla automatically when this computer starts?`
after the connect choice and runs `mewla boot install` (with `-lan` if you
chose the same Wi-Fi) when you press Enter. Without systemd user services, as
in many containers and WSL setups, it says so and keeps running in the
terminal.

`mewla boot install` refuses to run as root and refuses to start a second daemon
on a state directory that is already in use; stop the running `mewla` first.
Restarting the service never stops your `tmux` sessions or agents.

- **Linux**: to start it before you log in, enable lingering with
  `loginctl enable-linger` (the command tells you when this is needed).
- **macOS**: it starts when you log in and runs in your login session, the one
  that owns the screen and the privacy permissions. After a restart, log in
  once for Mewla to start. Output goes to `~/.mewla/logs/launchd.log`, readable
  only by you. A service never writes a pairing code there; run `mewla pair` to
  show one.
  The service runs a copy of the binary in `~/Applications/Mewla.app`, so
  System Settings and permission prompts show Mewla's name and icon. The
  command you type stays where you installed it.

  `mewla boot install` signs `Mewla.app` with a code-signing identity that it
  creates on this Mac the first time, so you grant Screen Recording and
  Accessibility once and they stay granted after updates. No Apple account
  is involved, and nothing asks you anything. The private key never leaves
  this Mac: it lives in its own keychain,
  `~/Library/Keychains/mewla-signing.keychain-db`, with a random password in
  `~/Library/Application Support/Mewla/signing/`. `mewla boot status` shows
  how `Mewla.app` is signed. If the identity can't be used, Mewla signs ad hoc
  instead, and macOS asks for the permissions again after each update.

Do not run the daemon as root.

## Defaults

| Setting | Value |
| --- | --- |
| Listen address | `127.0.0.1:9876` (`mewla --lan` uses `0.0.0.0:9876`) |
| State directory | `~/.mewla` |
| Executor configuration | `~/.mewla/executors.toml` (optional) |
| Brain workspace | `~/.mewla/brain` |

To use another state directory, pass the same `-state-dir` to every command:

```sh
mewla -state-dir /path/to/state
mewla pair -state-dir /path/to/state
```

## Install the app

### Android

Download `mewla-android-arm64-v<version>.apk` and `SHA256SUMS` from the
[GitHub Releases](https://github.com/daoleno/mewla/releases) and verify the APK:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

The APK supports 64-bit ARM phones (`arm64-v8a`) only. Android asks you to
allow installs from your browser or file manager, and Play Protect may warn
because Mewla is not on the Play Store. The official signing certificate's SHA-256
fingerprint is:

```text
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:
4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

#### Update the app

Mewla looks for a newer release on its own, at launch and when you come back to
it (at most every six hours). **Settings → About** shows the version and an
update row under it: **Check for updates** says **Up to date** and checks again
when you tap it, and offers **Retry** if it can't reach GitHub. When a newer
release exists, a small dot appears on the menu and on **Settings**, and the
row reads **Update available** with an **Update** button. Tap it to download the
APK, with progress in the row. Mewla checks the release's signed manifest and the APK's SHA-256
before it opens the Android installer. The first time, Android asks you to let
Mewla install apps. Allow it, come back, and the installer opens. If a download
fails, tap **Retry**. Versions before 0.2.9 need one manual install as
above; 0.2.9 and 0.2.10 show the update in **Settings → About** only.

Builds take their update channel from `MEWLA_ANDROID_DISTRIBUTION` at build
time: `github-apk` (default), `play-store` or `none`. Store and `none` builds,
and iOS, leave updates to the store and show the version only.

### iOS

Install **Mewla Preview** from
[TestFlight](https://testflight.apple.com/join/nMQheDCE): open the link on your
iPhone, install Apple's TestFlight app if asked, then tap **Accept** and
**Install**. TestFlight delivers new builds as they're released.

You can also build the app from source on an Apple Silicon Mac.

#### Build the iOS app from source

You need full Xcode, an iOS Simulator runtime or a signed device target,
CocoaPods, Bun, and network access for the first native build. Installing on a
physical iPhone also needs an Apple development team.

Clone the source, then from the repository root:

```sh
git clone https://github.com/daoleno/mewla.git && cd mewla
bun install
bun run native:build:ios     # builds the pinned terminal library
bun run native:verify:ios
cd app
bunx expo prebuild --platform ios --clean
cd ios && pod install && cd ..
bun run ios
```

You can also open `app/ios/Mewla.xcworkspace` in Xcode after `pod install`. The
app targets iOS 16.4 or newer and asks for local-network access (to reach your
daemon) and camera access (to scan pairing codes).

### Web UI

The daemon also serves the app as a web page for a browser on the same
computer or behind HTTPS. See [In a browser](connect-and-pair.md#in-a-browser).

## Build the daemon from source

With the Go version named in `daemon/go.mod` and Bun installed, from the
repository root:

```sh
bun install
bun run daemon:build   # writes bin/mewla
./bin/mewla doctor
```

A source build serves a placeholder instead of the web UI unless you first run
`bun run web:build`.

## Uninstall

1. Run `mewla boot uninstall` if you installed the service, and stop `mewla`.
   On macOS this also removes `~/Applications/Mewla.app`. It keeps the signing
   identity, so a later install keeps your permissions. To remove the identity
   too, run
   `security delete-keychain ~/Library/Keychains/mewla-signing.keychain-db`
   and `rm -r ~/Library/Application\ Support/Mewla/signing`, then remove
   **Mewla** from **Screen & System Audio Recording** and **Accessibility** in
   **System Settings → Privacy & Security**.
2. Delete the binary (`command -v mewla` shows where it is).
3. Optionally delete `~/.mewla`. It holds the daemon's identity, paired devices,
   Work and Brain data; deleting it unpairs every phone.
