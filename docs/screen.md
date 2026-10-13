# Screen

See and control your Mac's screen from the phone or a browser. Screen uses the
Mac's own Screen Sharing; Mewla carries it over the same signed connection as
everything else, so nothing new is exposed to the network.

Screen is off until you turn it on at the Mac.

## Turn it on

On the Mac:

1. **System Settings → General → Sharing → Screen Sharing**: on.
2. Click ⓘ next to it, then **Computer Settings…**: tick **VNC viewers may
   control screen with password** and set a password (up to 8 characters).
3. Keep Screen Sharing reachable only from the Mac itself:

   ```sh
   sudo defaults write /Library/Preferences/com.apple.RemoteManagement VNCOnlyLocalConnections -bool yes
   ```

   Then turn Screen Sharing off and on again. From another computer,
   `nc -w 3 <mac-address> 5900` should now print nothing.
4. Run:

   ```sh
   mewla screen enable
   ```

   It asks for the VNC password from step 2 and checks that it works.

`mewla screen status` shows whether a paired device can open the screen, and
`mewla screen disable` turns it off and forgets the password. Open viewers
close within seconds of `disable`.

Mewla needs no Screen Recording or Accessibility permission: Screen Sharing
captures the screen and applies input.

## Use it

Open **Screen** from the menu. It shows the current server's screen.

| Gesture | On the Mac |
| --- | --- |
| Tap | Click |
| Tap twice | Double-click |
| Press and hold, then lift | Right-click |
| Press and hold, then drag | Drag with the button held |
| Two-finger tap | Right-click |
| Two-finger swipe | Scroll |
| Pinch | Zoom the view on your phone (the Mac doesn't change) |
| One-finger drag while zoomed in | Move around the zoomed view |

**Use as trackpad** in the options menu switches to a pointer you push with
one finger, like a laptop trackpad. Tap clicks where the pointer is; tap, then
press and drag, drags.

The bar under the screen has the keyboard, the ⌘ ⌃ ⌥ ⇧ modifiers, Esc, Tab,
the arrows, Delete and Return, then common shortcuts. Tap a modifier once to
apply it to the next key, tap it twice to hold it, and a third time to release
it. To type Chinese or another language, switch the Mac's input source with
⌃ Space; the phone sends keys, not composed text.

The view rotates to landscape on this page only. The badge in the corner shows
the network round trip, the time from your tap until the screen redraws,
frames per second and bandwidth, with the Mac's power state below. Tap it to
hide it.

## Keep the Mac reachable

While Screen is on, Mewla keeps the Mac awake (`caffeinate -i -s`, the same as
macOS's own tool) so you can still reach it when you're away. The display may
still sleep; connecting wakes it, and a locked Mac shows its login window,
where you can type the password. **Let this Mac sleep** in the options menu,
or `mewla screen enable -allow-sleep`, turns this off. The hold ends when
Mewla stops.

macOS still sleeps when:

- a MacBook's lid is closed and no external display is connected. Keep the lid
  open; Screen warns you when it is closed.
- it runs on battery and the battery runs low. Leave the charger connected.

Wake for network access does not help: a sleeping Mac drops off Tailscale and
cannot be woken through it.

## Who can use it

Only devices you paired. A newer viewer takes over from an older one, and the
older one is told why. Every time a device connects or leaves, the Mac shows a
notification, and macOS shows its Screen Sharing icon in the menu bar while
anyone is connected. Revoking a device ends its session within seconds.

Screen is available on macOS hosts. The daemon talks only to `127.0.0.1`;
`mewla screen enable -address` accepts loopback addresses only.
