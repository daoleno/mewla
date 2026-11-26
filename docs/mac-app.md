# Mewla for Mac

Mewla for Mac is the app and the daemon in one download: a window for Brain and
your Sessions, a cat in the menu bar, and the daemon that runs the agents, which
keeps going when you close the window. It is signed with a Developer ID and
notarized by Apple.

It needs a Mac with Apple Silicon and macOS 14 or later. There is no build for
Intel Macs.

## Install

**Already use the CLI?** Use version 0.4.24 or later. Keep the CLI installed and open Mewla on a previously paired phone during the switch.

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

- The daemon is a login item (shown in **System
  Settings → General → Login Items & Extensions**). Quitting the app leaves it
  running, and so your Sessions and Workers. **Stop Mewla on this Mac…** in the
  cat's menu stops it and removes the login item.
- **This Mac** is always one of the app's computers. The app reaches it at its own
  loopback address, with no pairing step, and it cannot be removed. When its
  Mewla is not running, This Mac says so wherever it shows, and why when macOS
  will not start it (for example, Mewla turned off in Login Items). An older
  login item that macOS refuses to start is replaced when the app opens.
- The `mewla` command ([CLI](cli.md)) is the app's own, linked into
  `~/.local/bin/mewla`.
- The daemon's device key and other secrets are kept in your login Keychain
  under `com.daoleno.mewla`.

## If Mewla was installed with `install.sh`

Open the new app from **Applications**. It starts its daemon with the existing
identity and waits up to two minutes for a previously paired device to reconnect.
Open Mewla on that phone. When it reconnects, the app replaces the old login
item and links the CLI to its bundled helper. If the new daemon fails its
health check or no paired device reconnects in time, the app restores the
previous service. If you quit during the switch, reopen the app to recover the
previous service.

The 0.4.23 app could disconnect a Mac with an existing CLI installation.
If you already opened it and lost your connection, keep your Mewla state
directory and your phone's pairing when installing the new version.

## Updates

When a newer release exists, **Settings → General** shows **Update to X** beside
the version, and so does the cat's menu. One click downloads it (checked against
the update key built into the app), replaces the app, restarts the daemon onto
the new version and opens Mewla again. Sessions keep running, and the paired
devices and the login item stay as they were. If the daemon is still running
under the replaced app, macOS checks its permissions against that old copy, so
they read as **Restart Mewla** until it restarts. Mewla notices this and
restarts its daemon from the installed app at the next quiet moment, once.

## Permissions

Agents that look at or drive your screen need **Screen & System Audio
Recording** and **Accessibility** ([Screen](screen.md)). Both belong to Mewla
itself: the daemon runs as the app's child, so there is one Mewla in each list,
with its icon.

**Settings → Privacy & access** shows each one as macOS holds it now: Allowed,
**Allow**, or **Restart Mewla** when macOS applies a grant only to a new
process. **Allow** opens the permission's list in System Settings with a small
panel beside it holding Mewla's icon: drag it into the list, or turn Mewla on if
it is already there. The same panel brings Mewla back after you removed it with
−. A Mewla in the list that grants nothing (left by a differently signed Mewla)
is cleared first, so the Mewla you drag in is the one that counts. For
Accessibility, macOS reports such a row as switched off even when the list
shows it on, so a switched-off Accessibility row is also cleared and asked for
again. Once you give
the permission, the panel closes, Mewla comes back to the front
and applies it by itself, restarting its daemon where macOS needs that (Sessions
keep running).

When a Worker needs one you have not given, Brain's thread shows a card with the
reason and **Open System Settings**, which opens the same panel on this Mac.
macOS keeps these grants for the app's signature, so an update does not ask
again. A Worker that needs your Chrome gets the same kind of card
([Agents in your browser](your-browser.md)).

## Remove it

Choose **Stop Mewla on this Mac…** from the cat's menu, then move Mewla from
Applications to the Trash. Your Sessions, Work and settings are in `~/.mewla`.
