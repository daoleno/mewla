# Mewla for Mac

Mewla for Mac is the app and the daemon in one download: a window for Brain and
your Sessions, a cat in the menu bar, and the daemon that runs the agents, which
keeps going when you close the window. It is signed with a Developer ID and
notarized by Apple.

It needs a Mac with Apple Silicon and macOS 14 or later. There is no build for
Intel Macs.

## Install

1. [Download Mewla for Mac](https://github.com/daoleno/mewla/releases/latest/download/Mewla-macos-arm64.dmg)
   (the `.dmg` of the newest release).
2. Open the disk image and drag **Mewla** onto **Applications**.
3. Open Mewla. macOS asks once because the app came from the Internet; choose
   **Open**.

The first run starts the daemon, registers it as a login item so Mewla starts when
you log in, and shows Brain. You need one agent CLI; if the Mac has none, Brain says so and
offers to install Claude Code or Codex.

To use Mewla from your phone, choose **Pair a phone…** from the cat in the menu
bar and scan the code. On a fresh installation, choose **Allow trusted LAN** if
both devices are on a network you trust. Cancel keeps Mewla local. This allows
plain HTTP on the Mac's network interfaces until you choose **Settings →
Connections → Turn off LAN**, including after you close the dialog or restart.
The QR expires after 15 minutes. Devices that have paired keep their keys.
An existing phone route works without changing LAN access. See [Connect and pair](connect-and-pair.md).

## What runs

- The daemon is a login item (`com.daoleno.mewla.daemon`, shown in **System
  Settings → General → Login Items & Extensions**). Quitting the app leaves it
  running, and so your Sessions and Workers. **Stop Mewla on this Mac…** in the
  cat's menu stops it and removes the login item.
- The `mewla` command ([CLI](cli.md)) is the app's own, linked into
  `~/.local/bin/mewla`.
- The daemon's device key and other secrets are kept in your login Keychain
  under `com.daoleno.mewla`.

## If Mewla was installed with `install.sh`

Opening the app takes over: the old login job and `~/Applications/Mewla.app` are
removed, the daemon keeps its identity and every paired phone and browser stays
paired. The daemon is down for about a minute, once, while macOS lets go of the
old login item. From then on the app updates Mewla ([Install and update](install-daemon.md)),
and `mewla update` says so instead of replacing anything.

## Updates

When a newer release exists, **Settings → General** shows **Update to X** beside
the version, and so does the cat's menu. One click downloads it (checked against
the update key built into the app), replaces the app, restarts the daemon onto
the new version and opens Mewla again. Sessions keep running, and the paired
devices and the login item stay as they were.

## Permissions

Agents that look at or drive your screen need **Screen & System Audio
Recording** and **Accessibility** ([Screen](screen.md)). When a Worker needs one
you have not given, Brain's thread shows a card with the reason and **Open System
Settings**. macOS keeps these grants for the app's signature, so an update does
not ask again. A Worker that needs your Chrome gets the same kind of card
([Agents in your browser](your-browser.md)).

## Remove it

Choose **Stop Mewla on this Mac…** from the cat's menu, then move Mewla from
Applications to the Trash. Your Sessions, Work and settings are in `~/.mewla`.
