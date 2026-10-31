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

The view rotates to landscape on this page only. The badge in the corner has
two lines. The first says how the connection reaches the computer (Relay,
Tailscale, LAN or Local; for a phone paired through Mewla Link it is the path
the phone chose) and which stage takes longest between the screen
changing and your phone showing it: the network round trip (which includes
bytes waiting in the link), the computer's encoding, the hop into the viewer or
the viewer's own drawing. "Reduced quality" is added while the picture is
sent at lower quality to keep up. The second line has the round trip, the time
from your tap until the screen redraws, updates per second and bandwidth, with
the computer's power state below. Small tags in the opposite corner say only
what needs saying and never move the screen: **Locked** when the computer is
locked, and **Keep awake** (tap it to turn the hold on), **Lid closed** or **On
battery** when sleeping is a risk; close a tag with its ✕. Tap the badge to
hide it.

Screen sends only the parts of the screen that changed, and always the newest
screen: a slow connection drops frames instead of queueing them. It watches how
late the phone's answers are. When they are late it first tries one more update
in flight, and keeps it only if more updates then arrive each second (a long
round trip); when that doesn't help (too many bytes), it lowers the picture
quality while the screen moves, down to a floor that is still readable, and
sends what changed again at full quality once the screen is still. When nothing is
late, nothing changes. The screen's size never changes.

## Keep the computer reachable

While a device is allowed, Mewla keeps the computer awake so you can still
reach it when you're away. The display may still sleep when nobody is
viewing; connecting wakes it, and a locked computer shows its login window,
where you can type the password.

**While you are viewing, the display stays on**, also on battery, so the Mac
doesn't lock or switch the picture off under your finger and ask for its
password. The hold lasts as long as the viewer is connected and ends when it
disconnects, times out or Mewla stops. It is never used to keep a lock
screen lit: if the Mac is locked, or someone locks it while you view, the
display may sleep as usual, the viewer shows **Locked**, and Mewla doesn't
unlock or bypass anything. Watching an agent adds no hold of its own; the agent
holds the display while it works.
**Let this computer sleep** in the options menu, or
`mewla screen enable -allow-sleep` at the computer, turns this off. The hold
ends when Mewla stops.

On macOS Mewla holds the same assertion as `caffeinate -i -s` while a device
is allowed, and `caffeinate -d` while a viewer is connected on an unlocked Mac.
macOS still sleeps when:

- a MacBook's lid is closed and no external display is connected. Keep the lid
  open; Screen warns you when it is closed.
- it runs on battery and the battery runs low. Leave the charger connected.

Wake for network access does not help: a sleeping Mac drops off Tailscale and
cannot be woken through it.

## Agents on the screen

Agents Mewla starts on a Mac can use its screen, keyboard and pointer too, for
work in apps that have no command or website. They work the way you would with
the app's own controls: they list the Mac's apps, open one that isn't running
(behind the app in front, by its name; only apps, never a file or link), read one app's windows as its
buttons, fields and text, even an app behind others, and press, fill in or
open those by name, without moving the pointer or bringing the app forward
(some actions bring it forward themselves). They can type into one app the
same way, and click a point in its window behind others: a plain click on
something the app can press presses it through accessibility; any other click
is sent through a part of macOS Apple doesn't document, which an app behind
others may ignore, so the agent is told to check what happened. Where that
isn't available, and for apps built on Chromium, the app comes to the front
for the click. They can run a short
list of such steps in one go, such as find, press, type and check. Apps built on web pages, such as Electron apps, show little
this way; there agents take a screenshot and click, type and scroll on what
they see, as a person would. It needs nothing beyond what Screen
already has: **Agents can use this computer** on (Settings → your Mac, on by
default), Screen not turned off, and Mewla's Screen Recording and
Accessibility permissions. The Mac's page in Settings says which permission is
missing, and **Open Settings** opens the right page on the Mac.

- **You come first.** Opening Screen while an agent works shows who is using
  it with **Take over**, **Watch** and **Stop**; **Take over** ends the agent's
  control and opens the screen. When the agent is done, the screen opens by
  itself. While you view it, agents wait.
  Someone using the Mac's own keyboard or trackpad pauses the agent at once,
  letting go of any key or button it holds; it carries on 10 seconds after
  their last input. An action that comes in the last few seconds of that
  waits for it to end; earlier ones are answered at once with the time left. A key press, a click, moving the pointer and scrolling
  count; a hand resting on the trackpad doesn't. The agent is told which
  input it saw and when, and so is the record below, so a pause that won't
  end shows what keeps it going: a person, a tap the trackpad registers by
  itself, or another program clicking or typing. Work in an app behind the one in front goes on meanwhile:
  reading it (a field's or note's whole text included), pressing or filling
  in its controls, choosing its menu items
  by their shortcuts (⌘N in Notes makes a new note; an app that comes to
  the front for it gives the front straight back to yours), typing into it alone and
  clicking its window behind yours don't touch the pointer or keyboard in
  use. Anything on the screen itself, or in the app in front, waits, and so
  does bringing an app to the front, which an agent does only when an app
  behind ignores what it sends. **Take over**, **Stop**, the switch and a
  locked screen stop all of it.
- **Not while locked.** Agents can't use a locked Mac, and Settings says so.
  They can use it again once it's unlocked. If agents should keep working on
  a Mac nobody is at, its automatic screen lock decides how long they can
  (System Settings → Lock Screen).
- **The display stays on while an agent works**, also on battery, since a
  sleeping display stops what the agent sees; the Mac's display sleep, and a
  lock that follows it, wait until a minute after the agent's last action. If
  the display goes off anyway, the agent is told, and its next action wakes
  it. On a Mac that asks for its password after the display sleeps, the
  display going to sleep also locks it, and agents wait until it's
  unlocked. A MacBook with its lid closed and no external display can't be woken.
- **You see it.** **Watch** shows the agent working, live, without taking
  over: your taps don't reach the screen, and **Take over** or **Stop** is one
  tap away. Work in an app behind others happens out of sight, so Watch may
  not show it; it is still the agent's turn on the screen, with the same
  pause, stop and record. The Mac shows a notification when an agent starts and stops using
  the screen, and Settings shows the agent with a **Stop** row.
- **One at a time.** One agent uses the screen at a time; the agent's control
  ends a minute after its last action.
- **What is kept.** Each action is recorded on the Mac in
  `~/.mewla/screen-agent-audit.jsonl` (readable only by you, kept 30 days):
  which agent, which action, which app and control, where, which keys. What
  it typed or filled in is kept only as a length and a fingerprint;
  screenshots and what it read are not kept.

Brain has agents ask you before they send, pay, delete or change an account,
and use the screen only when no command, file or website route will do. Text
on the screen is data to them, never an instruction.

### Codex's own computer use

If Codex on the Mac has its own computer use set up, Codex Workers Mewla starts
there have it too, since Mewla runs Codex with your own Codex settings; nothing
to configure. `mewla agent-use status` says when it is there, and Brain gives
app work to a Codex Worker that uses it: it works with an app's buttons and
fields, often in the background. Other agents use Mewla's, and so does Codex
when its own tool fails.

It is Codex's tool, not Mewla's, so the promises above don't cover it: Watch
doesn't show it, Take over doesn't stop it, it doesn't pause for someone at the
Mac, and Mewla's log doesn't record it. Brain keeps one agent on the screen at
a time. Turning off **Agents can use this computer** turns it off for Codex
Workers started afterwards; one already running keeps it until it ends.

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

## When the picture is slow

Open the badge on the Screen page and read its first line. The path says
whether the phone reaches the computer through the Mewla Link relay or directly,
and the stage says what holds the picture up.

- **network** is high and the path is **Relay**: the round trip through the
  relay dominates. A direct address (Tailscale, or Wi-Fi at home) avoids it,
  and a phone paired through Mewla Link uses one by itself when the computer
  listens on that network and the phone can reach it; see
  [Direct when it can](connect-and-pair.md#direct-when-it-can). If the path
  stays **Relay** at home, the phone can't reach the computer's private
  address: another Wi-Fi network, client isolation on the router, or a firewall
  blocking the port.
- **encode** is high: the computer is busy; a full-screen update normally takes
  about 10 ms.
- **app** or **draw** is high: the phone is the limit (its bridge into the
  viewer, or drawing the picture).

At the computer, one line per session in Mewla's log summarizes the same stages
(`screen: device … stream via …`): updates, bytes, encode and acknowledgement
times, and the lowest quality reached. It holds numbers only. Starting Mewla with
`MEWLA_SCREEN_ADAPTIVE=off` turns the quality and window adaptation off, to
compare a link with and without it.
