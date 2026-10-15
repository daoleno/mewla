# Screen

See and control a computer's screen from the phone or a browser. Screen shows
the current server: the computer the Mewla daemon runs on. The picture and your
input travel over the same signed connection as everything else, so Screen
opens no new port and needs no extra password.

## Which computers

Screen works on Macs for now. On another computer the Screen page says
**Screen works on Macs for now**; in the app, **Choose a computer** opens
Settings, where you add the Mewla running on your Mac and switch to it.

| Computer | Status |
| --- | --- |
| macOS 13 or later (Apple Silicon) | Supported |
| Linux, Windows | Not yet |

## Turn it on

1. Open **Screen** from the menu and tap **Turn on Screen**.
2. At the computer, allow your device in the dialog that appears. Each device
   asks once; someone has to be at the computer to answer.
3. If the computer needs permissions, the Screen page lists them, and the
   operating system shows its own prompts. Grant them at the computer, or tap
   **Open settings** to show the right settings page there, then tap **Check
   again**. Some permissions apply only after Mewla restarts; the page then
   shows **Restart Mewla**, which restarts it on the computer and reconnects.

On macOS the permissions are **Screen Recording** (to see the screen) and
**Accessibility** (to click and type), both under **System Settings → Privacy
& Security**. When `mewla boot install` runs Mewla, both lists show **Mewla**
with its icon. Without it they show `mewla` or the terminal app that started
it. You grant them once: Mewla signs itself with an identity kept on this Mac,
so they stay granted after updates. If `mewla boot status` reports an ad hoc
signature, macOS asks again after each update; turn **Mewla** off and on in
both lists.

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
it. Text you type with the phone's keyboard, including Chinese and accented
letters, arrives as typed, whatever input source the computer uses. Keys
pressed with ⌘, ⌃ or ⌥ arrive as shortcuts.

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

## Agents on the screen

Agents Mewla starts on a Mac can use its screen, keyboard and pointer too, for
work in apps that have no command or website: they take a screenshot, then
click, type and scroll on what they see. It needs nothing beyond what Screen
already has: **Agents can use this computer** on (Settings → **Agents on**
*your Mac*, on by default), Screen not turned off, and Mewla's Screen Recording
and Accessibility permissions. That Settings section says which is missing and
opens the right settings page.

- **You come first.** Opening Screen while an agent works shows who is using
  it; **Take over** ends the agent's control and opens the screen. When the
  agent is done, the screen opens by itself. While you view it, agents wait.
  Someone using the Mac's own keyboard or trackpad pauses the agent at once,
  letting go of any key or button it holds; it carries on 10 seconds after
  their last input.
- **Not while locked.** Agents can't use a locked Mac, and Settings says so.
  Only someone at the Mac can unlock it; a Mac left to run agents all day
  needs its automatic screen lock off (System Settings → Lock Screen).
- **You see it.** **Watch** shows the agent working, live, without taking
  over: your taps don't reach the screen, and **Take over** or **Stop** is one
  tap away. The Mac shows a notification when an agent starts and stops using
  the screen, and Settings shows the agent with a **Stop** row.
- **One at a time.** One agent uses the screen at a time; the agent's control
  ends a minute after its last action.
- **What is kept.** Each action is recorded on the Mac in
  `~/.mewla/screen-agent-audit.jsonl` (readable only by you, kept 30 days):
  which agent, which action, where, which keys. What it typed is kept only as
  a length and a fingerprint; screenshots are not kept.

Brain has agents ask you before they send, pay, delete or change an account,
and use the screen only when no command, file or website route will do. Text
on the screen is data to them, never an instruction.

## Who can use it

Only paired devices that someone allowed at the computer, and agents Mewla
starts there while **Agents can use this computer** is on. A newer viewer takes
over from an older one, and the older one is told why; a viewer always takes
over from an agent. The computer shows a
notification every time a device connects or leaves. Revoking a device ends its
session within seconds.

At the computer:

```sh
mewla screen status     # support, permissions, allowed devices, keep-awake
mewla screen disable    # turn Screen off: viewers close and every device must be allowed again
mewla screen enable     # allow devices to ask again
```
