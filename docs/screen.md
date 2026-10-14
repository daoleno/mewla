# Screen

See and control a computer's screen from the phone or a browser. Screen shows
the current server: the computer the Mewla daemon runs on. The picture and your
input travel over the same signed connection as everything else, so Screen
opens no new port and needs no extra password.

## Which computers

Screen works where Mewla has a screen backend for the operating system.
Elsewhere the Screen page says **Screen isn't available on this computer yet**,
and nothing else changes.

| Computer | Status |
| --- | --- |
| macOS | In development |
| Linux, Windows | Not yet |

## Turn it on

1. Open **Screen** from the menu and tap **Turn on Screen**.
2. At the computer, allow your device in the dialog that appears. Each device
   asks once; someone has to be at the computer to answer.
3. If the computer needs permissions, the Screen page lists them, and the
   operating system shows its own prompts. Grant them at the computer, or tap
   **Open settings** to show the right settings page there, then tap **Check
   again**.

On macOS the permissions are **Screen Recording** (to see the screen) and
**Accessibility** (to click and type), both under **System Settings → Privacy
& Security**.

To start Mewla when the computer starts, so Screen is there after a restart,
see [Keep it running](install-daemon.md#keep-it-running).

## Use it

| Gesture | On the computer |
| --- | --- |
| Tap | Click |
| Tap twice | Double-click |
| Press and hold, then lift | Right-click |
| Press and hold, then drag | Drag with the button held |
| Two-finger tap | Right-click |
| Two-finger swipe | Scroll |
| Pinch | Zoom the view on your phone (the computer doesn't change) |
| One-finger drag while zoomed in | Move around the zoomed view |

**Use as trackpad** in the options menu switches to a pointer you push with
one finger, like a laptop trackpad. Tap clicks where the pointer is; tap, then
press and drag, drags.

The bar under the screen has the keyboard, the ⌘ ⌃ ⌥ ⇧ modifiers, Esc, Tab,
the arrows, Delete and Return, then common shortcuts. Tap a modifier once to
apply it to the next key, tap it twice to hold it, and a third time to release
it. To type Chinese or another language, switch the computer's input source
(⌃ Space on macOS); the phone sends keys, not composed text.

The view rotates to landscape on this page only. The badge in the corner shows
the network round trip, the time from your tap until the screen redraws,
frames per second and bandwidth, with the computer's power state below. Tap it
to hide it. Screen sends only the parts of the screen that changed and never
queues more than two frames, so the picture keeps up with your input on slow
connections.

## Keep the computer reachable

While a device is allowed, Mewla keeps the computer awake so you can still
reach it when you're away. The display may still sleep; connecting wakes it,
and a locked computer shows its login window, where you can type the password.
**Let this computer sleep** in the options menu, or
`mewla screen enable -allow-sleep` at the computer, turns this off. The hold
ends when Mewla stops.

On macOS Mewla holds the same assertion as `caffeinate -i -s`. macOS still
sleeps when:

- a MacBook's lid is closed and no external display is connected. Keep the lid
  open; Screen warns you when it is closed.
- it runs on battery and the battery runs low. Leave the charger connected.

Wake for network access does not help: a sleeping Mac drops off Tailscale and
cannot be woken through it.

## Who can use it

Only paired devices that someone allowed at the computer. A newer viewer takes
over from an older one, and the older one is told why. The computer shows a
notification every time a device connects or leaves. Revoking a device ends its
session within seconds.

At the computer:

```sh
mewla screen status     # support, permissions, allowed devices, keep-awake
mewla screen disable    # turn Screen off: viewers close and every device must be allowed again
mewla screen enable     # allow devices to ask again
```
