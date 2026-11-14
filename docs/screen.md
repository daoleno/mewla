# Screen

See and control a computer's screen from your phone or a browser, and watch
agents use it. Tap the cat, then **Screen**.

Screen shows the current computer: the one the Mewla daemon runs on. The
picture and your input travel over the same signed connection as everything
else, so Screen opens no new port and needs no extra password.

## Which computers

| Computer | Status |
| --- | --- |
| macOS 13 or later (Apple Silicon) | Supported |
| Windows 10 or 11 | Supported, with no permission to grant. Mewla has to run in your signed-in session (its sign-in task does); started as a service or over SSH, it can't see the screen and says so |
| Linux | Agents get displays of their own, off your desktop (see [below](#linux-agents-on-a-virtual-screen)); needs TigerVNC's `Xvnc` |

On any other computer, Screen says it isn't supported. To use another
computer's screen, switch to it: tap the cat, then the computer under
**Computers**.

## Turn it on

1. Tap the cat, then **Screen**, and tap **Turn on Screen**.
2. At the computer, allow your device in the dialog that appears. Each device
   asks once; someone has to be at the computer to answer.
3. If the computer needs permissions, Screen names them, and the operating
   system shows its own prompts. Grant them at the computer, or use Screen's
   button to open the right settings page there. Some permissions apply only
   after Mewla restarts; Screen then offers to restart Mewla on the computer and
   reconnects.

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

Screen fills the page, black in both themes. The bar on top has Back, the
computer and who has the controls (**Watching** or **You have the controls**),
**Take over** or **Give back**, and **⋯**:

- **Full size** fills the browser window and sends the keyboard to the
  computer, the browser's own shortcuts too where the browser allows. Hold Esc
  to leave.
- **Connection** shows how the picture reaches you (below).
- **Display** chooses which screen you see: a display of the computer, or on
  Linux an agent's monitor.
- **Phone** opens [Phone use](phone.md), when it is on.
- **Stop watching** closes the view.

When Screen can't show the picture, it says why with one button: the computer
is locked, its screen is off, Screen isn't turned on or allowed yet, or there is
no display.

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

With touch, the key bar under the picture has the keyboard, Esc, Tab, Ctrl,
Cmd, scroll and right click. Tap a modifier once to apply it to the next key,
tap it twice to hold it, and a third time to release it. Text you type with
the phone's keyboard, including Chinese and accented letters, arrives as typed,
whatever input source the computer uses. Keys pressed with a modifier arrive as
shortcuts. On a desktop or in the web app, the mouse, the wheel and the keyboard
go straight to the computer, and a paste sends your text there.

**⋯ › Connection** shows the way out if there is one (join the computer's Wi-Fi
or Tailscale, when the computer lists addresses the phone could not reach),
then the route (Relay, Tailscale, LAN or Local; for a phone paired through
Mewla Relay it is the path the phone chose, and **video** while motion is going
as video), the round trip, the time from your tap until the screen redraws,
updates per second, bandwidth, whether the picture is sent at reduced quality,
the stage that takes longest when it isn't the network (the computer's
encoding, the hop into the viewer or the viewer's own drawing), and the
computer's power state.

Screen sends only the parts of the screen that changed, and always the newest
screen: a slow connection drops frames instead of queueing them. It watches how
late the phone's answers are. When they are late it first tries more updates
in flight, and keeps them only if more updates then arrive each second (a long
round trip). Up to 4 are allowed; while the updates are small (about 16 KB or
less each, as when scrolling text, typing or moving the pointer) up to 16, since
with 4 in flight a 650 ms round trip, such as the relay across an ocean, caps
the picture at 6 updates a second however little there is to send. When that
doesn't help (too many bytes), it lowers the picture quality while the screen
moves, down to a floor that is still readable, and sends what changed again at
full quality once the screen is still. When nothing is late, nothing changes.
The screen's size never changes.

A connection that does not show a picture within about twenty seconds is
treated as lost: Screen says **Disconnected** and offers to reconnect, rather
than spinning for good.

### Video for motion

When much of the screen moves (a page scrolling, a video playing, a window being
dragged) the changes are too many bytes as pictures: 100 to 300 KB a frame, more
than a phone link carries. A viewer that can decode H.264 (RFB encoding 50, which
the viewer's noVNC offers when the phone's web view has WebCodecs) then gets that
motion as an H.264 stream from the computer's hardware encoder, a few KB a frame,
and gets the sharp picture back as soon as the screen is still. Text and small
changes (typing, the pointer) stay pictures. Input and the acknowledgements that
pace the stream are unchanged, and an agent's own use of the screen
(`mewla_computer`, screenshots, Watch) never uses video. The stream's bit rate
follows the link: it falls below what was actually delivered when answers get
late and rises again while they stay prompt.

Video needs a hardware H.264 encoder (every Apple Silicon Mac has one) and a
viewer that says it can decode it; otherwise nothing changes. It is **off by
default** until it has been tried on real machines: start Mewla with
`MEWLA_SCREEN_VIDEO=on` to turn it on, and `MEWLA_SCREEN_VIDEO=off` always
turns it off. **⋯ › Connection** says **video** while motion is going as
video, and the log line for a session says how many of its updates were.

## Keep the computer reachable

While a device is allowed, Mewla keeps the computer awake so you can still
reach it when you're away. The display may still sleep when nobody is
viewing; connecting wakes it, and a locked computer shows its login window,
where you can type the password.

**While you are viewing, the display stays on**, also on battery, so the Mac
doesn't lock or switch the picture off under your finger and ask for its
password. The hold lasts as long as the viewer is connected and ends when it
disconnects, times out or Mewla stops. It is never used to keep a lock screen
lit: if the Mac is locked, or someone locks it while you view, the display may
sleep as usual, Screen says the computer is locked, and Mewla doesn't unlock or
bypass anything. Watching an agent adds no hold of its own; the agent holds the
display while it works.

To let the computer sleep instead, run `mewla screen enable -allow-sleep` at
the computer; `mewla screen enable` keeps it awake again. The hold ends when
Mewla stops.

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
work in apps that have no command or website. It needs nothing beyond what
Screen already has: **Agents can use this computer** on (tap the cat, then the
computer's name; on by default), Screen not turned off, and Mewla's Screen
Recording and Accessibility permissions. The computer's details say which
permission is missing, with a button that opens the right page on the Mac.

They work the way you would, with the app's own controls:

- **Apps.** They list the Mac's apps and open one that isn't running (behind the
  app in front, by its name; only apps, never a file or link).
- **Reading.** They read one app's windows as its buttons, fields and text, even
  an app behind others. To find something they ask for only the elements that
  match (a role, a name, a value), or for what is inside one element or its top
  levels, rather than the whole app. After acting on a control, the agent is
  told what changed in the app since it last read it, not the whole app again;
  when a window opened or closed, or much changed, it gets the whole app.
- **Acting in the background.** They press, fill in or open controls by name,
  without moving the pointer or bringing the app forward (some actions bring it
  forward themselves). They type into one app the same way, and click a point
  in its window behind others: a plain click on something the app can press
  presses it through accessibility; any other click is sent through a part of
  macOS Apple doesn't document, which an app behind others may ignore, so the
  agent is told to check what happened. A drag from one point of a window to
  another goes the same way, and the app isn't made the active app, so your
  typing in front stays yours. Where that isn't available, and for apps built
  on Chromium, the app comes to the front for the click.
- **Steps.** They can run a short list of steps in one go, such as find, press,
  type and check, and wait for an element to appear, go away or become enabled.
- **Answers.** Each action says whether it was delivered: verified, sent and not
  verified (not to be repeated blindly), or refused (safe to retry). `check` and
  `uncheck` press only when the state isn't already the one asked. A text field
  that confirms (a file name in Finder, a search box) can hold the text before
  the app has taken it, so its answer says to send Return and read again.
- **Menus.** They choose or list an app's menu items by name
  (`File > Save As…`), in the background. A pop-up button (a team, a priority, a
  select) is chosen from in one step: `set_value` with the item's title opens
  its menu, takes the item and closes it again in tens of milliseconds, not
  across a model's turns.
- **The key guard.** An app behind your window that opens a menu makes macOS
  take the keyboard from your window while the menu is open, so your window
  resigns key and your typing is lost. Mewla can put a guard on your front app
  for as long as it acts on an app behind it, to keep your window from being
  told. It is off unless `MEWLA_SCREEN_KEYGUARD=drop` or `release` is set: on
  macOS 27 it has not been seen to hold anything, so it is an experiment. The
  answer says when it held the keyboard. The guard needs the Accessibility
  permission and macOS's window-server calls; without them the choice is still
  made, but your typing can be blocked while the menu is open.
- **Refused shortcuts.** Shortcuts and menu items that quit an app, log you out,
  shut down, restart or empty the Trash are refused unless you asked the agent
  for exactly that.
- **Web-page apps.** Electron apps such as Slack, VS Code and Cursor are read
  too: the first read of one switches on its web accessibility
  (`AXManualAccessibility`, confirmed by reading it back) and tells the agent;
  the app builds that part of its tree and may use more memory until it is
  restarted. Where an app still shows little, agents take a screenshot and
  click, type and scroll on what they see, as a person would.

The rules that keep you in charge:

- **You come first.** Opening Screen while an agent works shows it working
  (**Watching**). Your taps and typing still reach the screen; the agent keeps
  going and is told how many (never what you typed). **Take over** ends the
  agents' control and gives you the controls; while you have them, agents wait.
  Opened on the Mac
  itself (its own web UI), Screen never shows a live view, which would only
  mirror the page you are looking at, and no capture runs. From a phone or
  another computer the Mac's screen stays a live view.
- **Your hands don't pause it.** Someone using the Mac's own keyboard or
  trackpad does not pause the agent: nothing waits and nothing is refused. The
  agent's next answer says what you did (key presses, clicks, scrolls, how long
  ago, never what you typed) and whether it landed in the middle of its own
  action, and the agent looks at the screen again and judges whether it
  matters, because you may have tapped by mistake or be typing a password. A
  key press, a click, moving the pointer and scrolling count; a hand resting on
  the trackpad doesn't. The one mechanical rule is that a button or key the
  agent is holding is let go the moment you use the Mac, so it is never pressed
  under your hand, and the agent is told its drag or hold was interrupted. The
  record below notes the same.
- **Work behind your window goes on.** Reading an app behind the one in front
  (a field's or note's whole text included), pressing or filling in its
  controls, choosing its menu items by their shortcuts (⌘N in Notes makes a new
  note; an app that comes to the front for it gives the front straight back to
  yours), typing into it alone and clicking its window behind yours don't touch
  the pointer or keyboard in use. Anything on the screen itself, or in the app
  in front, goes on too, and so does bringing an app to the front, which an
  agent does only when an app behind ignores what it sends. Work behind other
  windows happens out of sight, so watching may not show it; it is still the
  agent's turn on the screen, with the same switches and record.
- **The hard switches.** **Take over**, **Agents can use this computer** and a
  locked screen stop all of it.
- **Not while locked.** Agents can't use a locked Mac, and the computer's
  details say so. They can use it again once it's unlocked. If agents should
  keep working on a Mac nobody is at, its automatic screen lock decides how
  long they can (System Settings → Lock Screen).
- **The display stays on while an agent works**, also on battery, since a
  sleeping display stops what the agent sees, and a Mac that asks for its
  password after the display sleeps locks itself then. It is held on from an
  agent's first action for as long as its Worker has a turn running, however
  quiet it is (a long build or wait included), and for ten minutes after the
  turn ends; a new turn in that time keeps it. Taking over, or the Worker being
  closed, lets it go at once, and an idle Worker left open does not keep the
  Mac awake. **⋯ › Connection** and `mewla agent-use status` say "Display kept
  on for agents" while it is held. Mewla never locks the Mac or changes its lock
  and password settings, and a lock you make yourself is yours to keep. Once
  the Mac is locked, agents wait until it is unlocked, and if the display goes
  off anyway the agent is told and its next action wakes it. A MacBook with its
  lid closed and no external display can't be woken.
- **You are told.** The Mac shows a notification when an agent starts and stops
  using the screen.
- **Several at once, each in its own apps.** Any number of agents can use the
  Mac at the same time as long as each works in a different app: they click and
  type into apps behind your windows, and those apps wait on the agent display.
  Neither waits for the other, and one agent's keys are never taken for yours.
  Only the pointer and keyboard themselves are one set, so one agent at a time
  acts on the screen itself; a second one waits its turn, and an agent that
  asks for an app another agent is working in is told which agent has it. Each
  agent's control ends a minute after its last action, but an app it moved to
  the agent display stays there, still its own, while its Worker's turn runs
  and for ten minutes after it ends, so its windows don't jump back onto your
  screen while it is thinking; they return at once when you take over, when
  its Worker is closed or its Work ends, or when its next action is
  `bring_forward`.
- **Closing a window behind others.** An app's Close menu item (cmd+w) is off
  while the app is behind other windows. The agent then presses the close
  button of the app's key window instead, without bringing anything forward.
  A document with unsaved changes asks first, with a sheet; the agent is told
  its buttons and nothing is chosen for it. It presses the one its task calls
  for, and discards a document only when told to.
- **What is kept.** Each action is recorded on the Mac in
  `~/.mewla/screen-agent-audit.jsonl` (readable only by you, kept 30 days):
  which agent, which action, which app and control, where, which keys, how
  long it took, and how big the answer was (with the number of lines and
  whether the app had more than was read). What it typed or filled in is kept
  only as a length and a fingerprint; screenshots and what it read are not
  kept.

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

It is Codex's tool, not Mewla's, so the promises above don't cover it: Screen
doesn't show it, Take over doesn't stop it, it isn't told what someone does at
the Mac, and Mewla's log doesn't record it. Brain keeps one agent on the screen
at a time. Turning off **Agents can use this computer** turns it off for Codex
Workers started afterwards; one already running keeps it until it ends.

### Windows

On Windows, agents use your screen as on a Mac: screenshots, the keyboard and
the pointer, and apps' buttons, fields and text through UI Automation, so
`app_state`, `element` and `steps` work there too (there is no `menu` tool).
Nothing needs a permission. Keys go only to the app in front: an agent setting
text in an app behind yours uses `element set_value`, which doesn't move your
focus. Windows refuses input to apps running as administrator, and the agent
is told so. Your own keys, clicks and pointer moves are told to the agent as on
a Mac, never what you typed; the lock screen, the sign-in screen and a UAC
prompt count as locked. Allowing a device to view the screen asks at the
computer in a Windows dialog.

## Linux: agents on a virtual screen

A Linux computer has no screen of yours to share, but the agents there get a
virtual screen of their own, off your desktop: one screen for the whole
computer with a monitor for each agent. An agent's browser window and the
desktop apps it opens are both on its monitor, and nothing else is.

On a Linux computer, **⋯ › Display** in Screen lists the agents using the
screen right now, and each one's live view shows only its own monitor (you are
never sent another agent's). An agent appears there when it asks for a browser
or opens an app; you never open or manage a browser yourself. **⋯ › Phone**
opens the phone, where Phone use is on.

- **The view is the computer.** It opens live and is the input surface: on a
  desktop or in the web app, the mouse (click, double click, right click, drag),
  the wheel and the keyboard (shortcuts included) go straight to the agent's
  monitor, a paste sends your text there, and **⋯ › Full size** fills the window
  and hands the browser's own shortcuts to the screen where it allows. Chrome
  on the screen is the real Chrome with its own address bar and tabs; use them.
  On a phone a tap clicks, a drag scrolls, a long press right-clicks (hold, then
  drag to drag), two fingers zoom and pan the view, and the keyboard key brings
  up the system keyboard, whose keys go straight to the screen.
- **The agent is told, not paused.** What you do reaches the screen at once
  while the agent keeps going. It is told what you did (clicks, scrolls, how
  much you typed, never what you typed) and decides for itself whether to go on,
  wait or ask you; nothing stops it by rule, since you may only have tapped by
  mistake or be typing a password.
- **Take over** is the one soft switch here: the agent's next action or browser
  call says you hold the screen, and it waits. **Give back** lets it go on, and
  it hears how long you held it. Leaving Screen gives it back by itself.

With **Settings › Agents › Virtual screen** on (tap the cat, then **Settings**), a
Worker (Claude or Codex) works in the agents' browser on the virtual screen
instead of your Chrome. It is the same browser tool every Worker has,
`mewla_chrome`, with the same Mewla extension in that Chrome, so nothing about
the tool differs from a Mac's. The browser comes up when the agent first asks
for it, not when the Worker starts, and the agent waits for it. It is one
Chrome with one profile; every agent has a window of its own in it, placed on
that agent's monitor (so are the windows its pages open, such as a sign-in
pop-up), and several agents work at the same time. The window closes when its
Worker ends, and the browser stops when no agent is using it. The switch lives
on the computer, so every device sees the same answer.

Desktop apps need no switch. A Worker (Claude or Codex) has the same computer
tool as on a Mac (`mewla_computer`): `open_app` starts an installed app (by the
name its menu entry shows, such as KWrite, or its program) on the agent's own
monitor, `app_state` reads the app's buttons, fields and text through AT-SPI,
and `element`, `steps`, screenshots, clicks and keys act there. **⋯ › Display**
lists the agent as soon as it has a monitor, with the same live view and Take
over. The
apps join an accessibility bus private to the agent screen, so nothing of your
desktop's accessibility is used or switched on (it needs `at-spi2-core`; without
it the agent works from screenshots). Without `Xvnc` the agent has no computer
tool on Linux, and with the virtual screen switch on but the computer tool off
it has the older **display** tool instead: coordinates only, on its monitor.

The screen is private to Mewla and off your desktop; it needs TigerVNC's
`Xvnc` (and `xauth`) on the computer, and apps run as X11 clients (there is no
window manager: Mewla moves each app's window to its agent's monitor and a
window keeps the size it opens with). The picture travels over a Unix socket
only your user can open, no network port is used, and an agent's monitor is
given up when its Worker ends or closes it.

The browser has one profile for all agents on the computer, not your everyday
Chrome: to give it a sign-in, take over an agent's screen, sign in yourself and
give it back; every agent's window then has it, because they are windows of the
same Chrome. The profile keeps its sign-ins on that computer, also when the
browser stops. Chrome stores them in the computer's secure wallet (KWallet or
another Secret Service) when one is unlocked for the user Mewla runs as; with
none, the profile uses Chrome's own basic storage, in a folder only that user
can read. The profile keeps the store it started with, so one that began on the
wallet waits for the wallet to be unlocked rather than lose its sign-ins. See
[Agents in your browser](your-browser.md) for the browser that uses your own
Chrome and sign-ins on your desktop instead.

## The floating screen

While the agent of the Session or Work you are looking at uses a screen, that
screen floats over the page as a small live window: its Linux display, the
Mac's screen, or the phone (see [Phone use](phone.md)). It is one window with
one set of gestures for all three. On a page that belongs to no Session or Work
it shows whichever agent uses a screen. It disappears when the agent is done
with the screen, and on that screen's own page.

- **Move it** by dragging: it snaps to a corner and remembers its corner and
  size. It starts at the top right, under the header, on pages with a composer
  or key bar; on a wide window, in the right pane's bottom-right corner. It
  stays above the composer and the keyboard.
- **Tap the title** to go to the next screen when several agents use one.
  **Double-tap** to go between the default and a large size. The handle in the
  inner corner resizes it, the shape locked. **Open** opens that screen's own
  page: Screen, or Phone use for the phone.
- **The picture is the input surface**, as in Screen. On a Linux display a tap
  clicks and the keyboard key types; the agent is told, not paused. The Mac's
  float is for watching: taking over stays in Screen.

## Who can use it

Only paired devices that someone allowed at the computer, and agents Mewla
starts there while **Agents can use this computer** is on. A newer viewer takes
over from an older one, and the older one is told why; a viewer always takes
over from an agent. The computer shows a notification every time a device
connects or leaves. Revoking a device ends its session within seconds.

At the computer:

```sh
mewla screen status     # support, permissions, allowed devices, keep-awake
mewla screen disable    # turn Screen off: viewers close and every device must be allowed again
mewla screen enable     # allow devices to ask again
```

## When the picture is slow

Open **⋯ › Connection**. The route says whether the phone reaches the computer
through Mewla Relay or directly, and the slowest stage, when it isn't the
network, says what holds the picture up.

- **Network** is high and the route is **Relay**: the round trip through the
  relay dominates, and it is set by where the relay is: a relay on another
  continent adds its distance twice (phone to relay, relay to computer), so
  even a few KB takes 400 to 700 ms to be answered. Input then reaches the
  screen that much later too; only a nearer relay or a direct address
  shortens that. A direct address (Tailscale, or Wi-Fi at home) avoids it,
  and a phone paired through Mewla Relay uses one by itself when the computer
  listens on that network and the phone can reach it; see
  [Direct when it can](connect-and-pair.md#direct-when-it-can). If the path
  stays **Relay** at home, the phone can't reach the computer's private
  address: another Wi-Fi network, client isolation on the router, or a firewall
  blocking the port. When the computer listed private addresses and none could
  be verified from this phone, Connection says "Join your computer's Wi-Fi or
  Tailscale" (turn Tailscale on on the phone, or join the same Wi-Fi). When the
  computer lists none (no private network address, or it was started on a
  non-loopback address), the phone can't change that and the details give no
  fix.
- **Slowest** reads **encode**: the computer is busy; a full-screen update
  normally takes about 10 ms.
- **Slowest** reads **app** or **draw**: the phone is the limit (its bridge into
  the viewer, or drawing the picture).

At the computer, one line per session in Mewla's log summarizes the same stages
(`screen: device … stream via …`): updates, bytes, encode and acknowledgement
times, and the lowest quality reached. It holds numbers only. Starting Mewla with
`MEWLA_SCREEN_ADAPTIVE=off` turns the quality and window adaptation off, to
compare a link with and without it.
