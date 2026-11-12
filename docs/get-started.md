# Get started

From nothing to your first goal in about ten minutes. This page is one path;
[Install and update](install-daemon.md) and [Connect and pair](connect-and-pair.md)
cover the alternatives.

## The pieces

- **The daemon** (`mewla`) runs on your computer next to your repositories and
  agent CLIs. It owns Sessions, Brain, Work and pairing, and runs every agent
  in its own session host, so nothing else, such as `tmux`, is needed. One
  binary; no cloud service is required, and remote access is an opt-in sign-in.
- **The app** on Android or iOS connects to one computer at a time. The daemon
  also serves it as a web page.
- **Your agent CLIs** (Claude Code, Codex, Cursor Agent, Grok, Pi, OpenCode)
  do the work. Mewla starts and reads them; you sign in to each yourself.

Your code, keys and agent state stay on your computer.

## What you need

- Linux (`amd64` or `arm64`), WSL2, an Apple Silicon Mac, or Windows 10
  (version 1809 or later) or 11 on x64 (64-bit Intel or AMD)
- One signed-in agent CLI. On Linux and macOS, `mewla` offers to install one;
  on Windows, install and sign in to one yourself first
- An arm64 Android phone, an iPhone, or a browser

## 1. Install the daemon

On Linux, WSL2 or macOS:

```sh
curl -fsSL https://mewla.ai/install.sh | sh
```

It verifies the release checksum, installs `mewla` without `sudo` and sends no
telemetry. The last line is the command that starts Mewla: `mewla`, or its
full path when your current shell cannot find it yet.

On Windows, in PowerShell:

```powershell
irm https://mewla.ai/install.ps1 | iex
```

It installs Mewla for your user only, with no administrator rights and no WSL,
adds it to your `PATH`, starts it, and starts it again each time you log in.

## 2. Start it and pair your phone

```sh
mewla
```

The first time, it checks this computer before anything else. Without a
signed-in agent CLI on Linux or macOS, it offers Claude Code or Codex with
their official installers, then their sign-in (`claude auth login` or
`codex login`). One ready agent is enough, and Brain uses it. With more than
one ready, it asks once which agent Brain should use, Claude Code first; change
it any time with `mewla brain use <agent>`.

Then it asks how your phone and browsers should reach this computer. Pick 1
and sign in with GitHub: the computer gets its own address, such as
`https://quiet-otter-3f9a.m.mewla.ai`, that works from any network. Last, on
Linux and macOS, it asks whether to start Mewla automatically with the
computer, using [`mewla boot install`](install-daemon.md#keep-it-running). On
Windows the installer has already done that.

With no device paired yet, it shows one screen: whether remote access is on,
a QR code for your phone, when the code expires, and a short link for a browser
on this computer.

On the phone:

1. Install the app: the Android APK from
   [Releases](https://github.com/daoleno/mewla/releases), or
   [iOS on TestFlight](https://testflight.apple.com/join/nMQheDCE). Android
   asks you to allow installs from your browser or file manager the first time
   you open the APK; allow it for that app, then install.
2. Open it. On the welcome screen, tap **Mewla already running? Scan its code**. (If
   you haven't installed the daemon yet, **Set up my computer** shows the
   command first, then **Next: scan the code**.)
3. Scan the code, then tap **Pair and grant access** on **Pair this phone?**.
4. When it says **Brain is home.**, tap **Open Brain**.

The code works once and expires; `mewla pair` prints a fresh one. For the web
UI on this computer, run `mewla web`: it opens the browser already paired.

Other ways to connect, such as the same Wi-Fi only, Tailscale or your own
HTTPS address, are in [Connect and pair](connect-and-pair.md#choose-an-address).

## 3. Start a Session

Tap the state control next to **Brain** at the top (it reads **All quiet** when
nothing runs) to open the tray, then tap **+**. Pick one of your repositories and tap an agent.
Ask it something small.

The **Chat / Terminal** switch in the Session's header changes the view; both
show the same Session, and the app remembers your choice for it. You can also
open it at your desk with `mewla attach` (pick a Session from the list, or name
one: `mewla attach brain`; Ctrl-\ detaches and leaves it running), or all of
them at once with `mewla attach --all`. More in [Sessions](sessions.md).

## 4. Give Brain a goal

Brain is the home screen. Tell it something with a few parts, for example "Fix
the failing test in sync/, then update the changelog". Brain splits it into
Work and starts Workers. Each Work shows as a card in Brain's conversation and
as a row in the tray, and the state control counts what is running.

When a Work needs your decision, the card asks in the conversation, the tray
lists it under **Needs you**, and your phone gets a notification. Answer in any
one of them. More in [Brain and Work](brain-and-work.md).

Workers run without asking for approval. Before you leave Brain working on a
machine with secrets, read [Permission bypass risks](executors.md#permission-bypass-risks).

## Find your way

Home is Brain's conversation. The header holds three things:

- **The cat**, at the left: the computer page. It shows this computer and its
  connection, anything that **Needs fixing**, your other **Computers** and
  **Pair a computer**, then **Screen**, **Extensions**, **Stats**,
  **Resources** and **Settings**.
- **The state control**, next to **Brain**: the tray. It lists what **Needs
  you**, what is **Running**, **Today**'s calendar, **Idle** and **Done**, with
  **+** for a new Session, **Calendar** and **All Sessions**.
- **⋯**, at the right: Brain's menu, with **Open terminal**, **Browse
  workspace**, **Switch executor**, **Model** and **New chat**.

On a window 1024 points wide or more (a tablet in landscape, an unfolded
foldable, a desktop browser), the tray stays open as a left pane, with the cat
on top, and the page you open sits on the right.

## Next

- Keep the daemon running across reboots: [Install and update](install-daemon.md#keep-it-running)
- Reach it from anywhere, add a tablet or a browser: [Connect and pair](connect-and-pair.md)
- Get a push only when something needs you, or talk to Brain from Telegram: [Notifications and Telegram](notifications.md)
- Choose models and keys, or lock agents down: [Agents and models](executors.md)
