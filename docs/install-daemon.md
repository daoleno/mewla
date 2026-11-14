# Install and update

Everything [Get started](get-started.md) skips: platforms, manual downloads,
running as a service, updates, the app builds and uninstalling.

## What you need

- Linux `amd64`/`arm64`, WSL2, an Apple Silicon Mac, or Windows 10 (version
  1809 or later) or 11 on x64 (beta)
- `curl`, `tar`, and `sha256sum` (Linux) or `shasum` (macOS); on Windows,
  PowerShell
- At least one agent CLI on `PATH` and already signed in: `claude`, `codex`,
  `cursor-agent`, `grok`, `pi` or `opencode` (OpenCode 2; `mewla doctor` names
  the upgrade command when it finds OpenCode 1). One is enough. On Linux and
  macOS, `mewla` offers Claude Code and Codex when none is ready; on Windows,
  install one yourself. `mewla` asks which one Brain uses when several are
  ready.

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

The last line is the command that starts Mewla: `mewla`, or its full path
when the current shell cannot find it yet. New terminals find `mewla` once the
installer has added its directory to your shell profile.

If `mewla` is already installed, `install.sh` runs `mewla update` instead,
unless you set `MEWLA_VERSION` or `MEWLA_INSTALL_DIR`.

Options, set as environment variables on `sh`:

| Variable | Effect |
| --- | --- |
| `MEWLA_VERSION=v0.4.3` | Install this exact release tag, beta included, instead of the newest stable |
| `MEWLA_INSTALL_DIR="$HOME/bin"` | Install into this user-owned directory |
| `MEWLA_NO_PATH_UPDATE=1` | Do not add the install directory to your shell profile |
| `MEWLA_DRY_RUN=1` | Print the platform, version and destination; change nothing |

```sh
curl -fsSL https://mewla.ai/install.sh | MEWLA_INSTALL_DIR="$HOME/bin" sh
```

### Windows

In PowerShell, as yourself (not as Administrator):

```powershell
irm https://mewla.ai/install.ps1 | iex
```

It finds the newest stable release the same way, verifies
`mewla-windows-amd64.zip` against `SHA256SUMS`, and installs `mewla.exe` into
`%LOCALAPPDATA%\Programs\Mewla`. It adds that folder to your user `PATH`, then
runs [`mewla boot install`](#keep-it-running): Mewla starts now and each time
you log in. Nothing needs administrator rights. Running it again installs over
the existing copy, even while Mewla is running, but the running Mewla keeps its
old version until it restarts; to update, use `mewla update`.

Windows is in beta. Tell us how it went, good or bad:
[open an issue](https://github.com/daoleno/mewla/issues/new?title=Windows%3A%20&body=Windows%20version%3A%0A%0Amewla%20version%3A%0A%0Amewla%20doctor%3A%0A%0AWhat%20happened%3A%0A) with your Windows version and the
output of `mewla version` and `mewla doctor`.

`irm | iex` takes no arguments, so options are environment variables set
before it:

| Variable | Effect |
| --- | --- |
| `MEWLA_VERSION=vX.Y.Z` | Install this exact release tag (`-beta.N` too) |
| `MEWLA_INSTALL_DIR=<path>` | Install into this folder (not a system folder) |
| `MEWLA_NO_PATH_UPDATE=1` | Do not change your user `PATH` |
| `MEWLA_NO_START=1` | Do not start Mewla or register it to start at login |
| `MEWLA_DRY_RUN=1` | Print the plan; change nothing |
| `MEWLA_INSTALL_FROM=<folder>` | Use `mewla-windows-amd64.zip` and `SHA256SUMS` from this folder instead of downloading |

```powershell
$env:MEWLA_NO_START = '1'; irm https://mewla.ai/install.ps1 | iex
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

On Windows, compare the hash with the `mewla-windows-amd64.zip` line of
`SHA256SUMS`, then unpack `mewla.exe` into a folder on your `PATH`:

```powershell
(Get-FileHash mewla-windows-amd64.zip -Algorithm SHA256).Hash.ToLower()
```

If macOS blocks the binary,
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
   into the app (on the welcome screen, or the computer page's **Pair a computer**).
4. **Wait.** It waits up to 15 minutes, until the code expires, for the phone,
   then says so.

If no agent CLI is signed in on that computer yet, it ends by naming the step:
`mewla deps`. Brain cannot answer until one is.

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
| Windows 10 (1809 or later) and 11, x64 | `mewla-windows-amd64.zip` | Beta, since 0.3.0 |
| Windows on ARM | None | Not supported |
| Intel macOS | None | Not supported |

Both installers stop on an unsupported platform before downloading anything.
On Windows, use `install.ps1`; `install.sh` is for Linux, WSL2 and macOS.

## Check the host

```sh
mewla doctor          # platform, state directory, listen port, agent CLIs
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
keeping your address, directories and `PATH`. Your Sessions and agents keep
running: they live in the session host, not in the daemon.

### Automatic updates

A released `mewla` on Linux and Windows checks every six hours and installs a
newer stable release once no Brain turn or Worker is running. When a newer
release exists, the cat on home carries a small ink dot and the computer page
(tap the cat) shows an update row with **Update**, which does the same right
away; once you have seen that row, the cat's dot goes until the next release.
The computer's details show the version it runs. When the app and the computer no longer fit together,
**Needs fixing** reads **Update required** with **Update**, and the cat
carries the red dot instead.

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

### Updating from Mewla 0.2

Mewla 0.3 runs every agent in its own session host and no longer needs `tmux`.
Sessions an earlier Mewla started in `tmux` keep running through the update,
and you can still read and type into them. Each one moves to the session host
on its own once its agent finishes its turn: the agent starts again in the
same conversation, under the same Session, with its Work. An agent Mewla cannot
resume this way (Grok, Cursor Agent, a plain shell) keeps running in `tmux`,
no longer controlled by Mewla; `mewla attach` still lists them. If
Brain itself has to stay behind, a new Brain starts in the session host and
picks up from its handoff notes and your Work. You can uninstall `tmux` once
none are left that you need.

## Keep it running

`mewla` is the whole runtime. Run it in a terminal, or let
Mewla start it for you: a user service on Linux with systemd, a LaunchAgent on
macOS, a scheduled task for your user on Windows.

```sh
mewla boot install          # this binary, default state and address
mewla boot install -lan     # same, listening on the private network
mewla boot install -dry-run # print the service without installing it
mewla boot status
mewla boot uninstall
```

On Linux and macOS, the first `mewla` asks `Start Mewla automatically when
this computer starts?` after the connect choice and runs `mewla boot install`
(with `-lan` if you chose the same Wi-Fi) when you press Enter. Without systemd
user services, as in many containers and WSL setups, it says so and keeps
running in the terminal. On Windows the installer runs `mewla boot install`
itself.

`mewla boot install` refuses to run as root and refuses to start a second daemon
on a state directory that is already in use; stop the running `mewla` first.
Restarting the service never stops your Sessions or agents.

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
- **Windows**: a Task Scheduler task named `Mewla` for your user only, which
  starts Mewla when you log in, with no window and no administrator rights.
  Installed over SSH with nobody logged in at the desktop, Mewla starts now
  and the task takes over from your next login. Output goes to
  `logs\daemon.log` in the state directory.

Do not run the daemon as root.

## Defaults

| Setting | Value |
| --- | --- |
| Listen address | `127.0.0.1:9876` (`mewla --lan` uses `0.0.0.0:9876`) |
| State directory | `~/.mewla` (on Windows, `%USERPROFILE%\.mewla`) |
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
it (at most every six hours). **Settings › About** shows the version and an
update row under it: **Check for updates** says **Up to date** and checks again
when you tap it, and offers **Retry** if it can't reach GitHub. When a newer
release exists, the row moves to the top of **Settings** and reads **Update
available** with the version and an **Update** button. The cat on home carries
a small ink dot until you have opened Settings once for that version, and the
computer page's **Settings** row carries the same dot until the update is
installed.

Tap **Update** to download the APK, with progress in the row. Mewla checks the
release's signed manifest and the APK's SHA-256, then opens the Android
installer. The first time, Android asks you to let Mewla install apps: the row
reads **Allow app installs** with **Continue**. Allow it, come back, and the
installer opens. If you close the installer, the row offers **Install** again;
if a download fails, it offers **Retry**.

Only the GitHub APK updates itself. A Play Store build (`-Pmewla.channel=play`)
leaves updates to the store and shows the version only.

### iOS

Install **Mewla Preview** from
[TestFlight](https://testflight.apple.com/join/nMQheDCE): open the link on your
iPhone, install Apple's TestFlight app if asked, then tap **Accept** and
**Install**. TestFlight delivers new builds once Apple's beta review passes.
When a newer build is in TestFlight, the row at the top of **Settings** reads
**Update available**, and **Update** opens it in TestFlight; the cat and the
computer page's **Settings** row carry the same dot as on Android.

You can also build the app from source on an Apple Silicon Mac.

#### Build the iOS app from source

You need full Xcode, an iOS Simulator runtime or a signed device target,
xcodegen, and network access for the first native build. Installing on a
physical iPhone also needs an Apple development team.

Clone the source, then from the repository root:

```sh
git clone https://github.com/daoleno/mewla.git && cd mewla
./scripts/build-libghostty-ios.sh     # builds the pinned terminal library
./scripts/verify-libghostty-ios.sh
client/scripts/fetch-nsh.sh --ios     # NSH and its iOS framework
client/scripts/ios-project.sh         # generates client/ios/Mewla.xcodeproj
open client/ios/Mewla.xcodeproj
```

Build and run the Mewla scheme from Xcode. The app asks for local-network
access (to reach your daemon) and camera access (to scan pairing codes).

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

On Windows:

1. Run `mewla boot uninstall`. It stops Mewla and removes the scheduled task.
   Your Sessions keep running in the session host until you sign out or
   restart, and Windows can't delete `mewla.exe` while they do.
2. After signing out or restarting, delete `%LOCALAPPDATA%\Programs\Mewla`
   and remove that folder from your user `PATH`.
3. Optionally delete `%USERPROFILE%\.mewla`.
