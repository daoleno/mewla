# Screen

See and control a computer's screen from your phone or a browser, and watch
agents use it. Tap the cat, then **Screen**.

Screen shows the current computer: the one the Mewla daemon runs on. The
picture comes as video that the computer pushes the moment its screen changes,
and your input goes back beside it, encrypted end to end. Both are set up over
the same signed connection as everything else, so Screen needs no extra
password; they go straight between your phone and the computer when one can
reach the other, and through Mewla Relay otherwise (see
[How the picture travels](#how-the-picture-travels)).

## Which computers

| Computer | Status |
| --- | --- |
| macOS 13 or later (Apple Silicon) | Supported |
| Windows 10 or 11 | Supported, with no permission to grant. Mewla has to run in your signed-in session (its sign-in task does); started as a service or over SSH, it can't see the screen and says so |
| Linux | Agents get displays of their own, off your desktop (see [below](#linux-agents-on-a-virtual-screen)); needs TigerVNC's `Xvnc` and `ffmpeg` |

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

Screen fills the page, black in both themes, and shows one screen at a time:
the computer's own, or one agent's (see [Agents' displays](#agents-displays)).
While an agent uses the screen you see, it has one switch, the same everywhere:

- **Take over** gives you the controls; while you have them the agent waits,
  and a red frame goes round the picture.
- **Give back** lets the agent go on (it is told how long you had it): Screen
  lets go of the computer's screen at once and shows the agent again as soon as
  it uses it. Leaving Screen gives it back by itself.
- **Stop**, the square button beside it, ends what the agent is doing on the
  screen and tells it that you pressed Stop (its next action, if none was under
  way, is refused saying so). It is not locked out: like everything you do on
  an agent's screen, Stop informs the agent, and it uses the screen again only
  if you ask it to.

On a phone the switch sits under the picture and steps aside while you type; on
a desktop or a wide window it is in the bar on top. The bar also has Back, the
screen you see, the keyboard and **⋯**:

- **Fit** shows the whole picture again.
- **Full size** fills the browser window and sends the keyboard to the
  computer, the browser's own shortcuts too where the browser allows. Hold Esc
  to leave.
- **Connection** shows how the picture reaches you (below).
- **Phone** opens [Phone use](phone.md), when it is on.
- **Disconnect** closes the view.

When Screen can't show the picture, it says why with one button: the computer
is locked, its screen is off, Screen isn't turned on or allowed yet, or there is
no display.

| Gesture | On the computer |
| --- | --- |
| Tap | Click, with a ring where it landed |
| Tap twice | Zoom the view in there, or back out (the computer doesn't change) |
| Press and hold, then lift | A menu: **Right click**, **Double click** or **Drag** (the next drag holds the button) |
| Press and hold, then drag | Drag with the button held |
| One-finger drag | Scroll; zoomed in, move around the view instead |
| Two-finger tap | Right-click |
| Pinch | Zoom the view on your phone |

The keyboard button brings up the phone's keyboard, and a bar of the keys it
lacks docks above it: **Paste** (types what the phone copied, such as a
password or a code), ⌫, ↵, Tab, Esc, the modifiers and the arrows. Tap a
modifier once to apply it to the next key, tap it twice to hold it, and a third
time to release it. A line above the bar shows what you type, each character
for a second before it turns into a dot. The picture keeps its size when the
keyboard comes up and moves so the field you tapped stays in sight. Text you
type, including Chinese and accented letters, arrives as typed, whatever input
source the computer uses; keys pressed with a modifier arrive as shortcuts. On
a desktop or in the web app, the mouse, the wheel and the keyboard go straight
to the computer, and a paste sends your text there.

**⋯ › Connection** shows the way out if there is one (join the computer's Wi-Fi
or Tailscale, when the computer lists addresses the phone could not reach),
then the route the video takes (Relay, Tailscale, LAN or Local), the round
trip, the time from your tap until the picture changes, frames per second,
bandwidth, the computer's encoding when that is what holds the picture up, and
the computer's power state.

The computer sends a picture the moment its screen changes, up to 60 a second,
and always the newest: on a slow link pictures are skipped, never queued. The
video's rate follows what the link carries, falling at once when the link fills
and rising again while it holds, so motion stays smooth and only goes softer on
a thin link. When the screen stops moving, the computer sends the still picture
again ten times in the next second, each sharper than the last, and then nothing
until something changes: small text is readable at full size within that
second. The first picture of a view is a whole one (a key frame), and so is the
next after one was lost, so a view never waits on a broken stream. The
computer's screen never changes size; an agent's browser window does while you
take it over from a phone (see [below](#agents-displays)).

A connection that does not show a picture within about twenty seconds is
treated as lost: Screen says **Disconnected** and offers to reconnect, rather
than spinning for good.

### How the picture travels

The picture is H.264 video over WebRTC, played by the phone's or browser's own
video decoder the moment it is decoded, with no playout buffer. The computer
encodes with its own hardware where it has an encoder (VideoToolbox on a Mac,
Media Foundation on Windows) and with `ffmpeg` on Linux (VA-API first, then
x264), without B-frames, so each picture leaves as soon as it is encoded.

Your input goes back on the same connection, in two channels: keys, clicks,
the wheel and paste in order and never lost, and pointer moves unordered and
never resent, so a lost move is simply overtaken by the next.

The connection is set up over Screen's signed socket: the computer offers, the
viewer answers. It goes directly between the phone and the computer on the same
Wi-Fi/LAN or over Tailscale, and through Mewla Relay's TURN service otherwise
(mobile data, a strict network): over UDP, or TCP, or TLS on port 443 where UDP
is blocked. **⋯ › Connection** names the route: **LAN**, **Tailscale**,
**Local** or **Relay**.

**End to end.** The video and your input are encrypted with DTLS-SRTP between
your device and the computer; the relay forwards packets it cannot read. Each
side signs the fingerprint of its DTLS certificate: the computer with its key
from pairing, which the app checks, and your device with its own key, which the
computer checks against the device that opened the view. The encryption is held
to those two certificates, so nothing in between (the relay, a network) can
read the picture or your input, or take either side's place.

**The relay's TURN.** A computer asks its relay for TURN credentials when an
allowed device opens a view, and hands them to that view; each lasts 12 hours,
and the computer asks again once half of that has passed or the relay restarted.
With one, the relay relays UDP only, to public addresses and to the other side's
allocation on the same relay: never to the relay's own other ports, private or
shared networks such as a tailnet's, or cloud metadata. It holds
each credential to a few allocations, each computer to a few more, and each
allocation to a bit rate; the computer keeps its video under that rate while
the picture goes through the relay.

An agent's own use of the screen (`mewla_computer`, screenshots, Watch) never
goes through any of this.

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
- **Agents' browser windows.** An agent that only uses the browser
  (`mewla_chrome`) gets a window of its own in your Chrome, on the agent
  display, off your screen. Screen shows it at once under the agent's name,
  before your Mac's own screen (in the list of agents and in **⋯**, and back
  from **⋯**), the same way a Linux computer shows its agents' displays: a live
  view of just that window, typing and clicks that reach that window only
  (never another of your Chrome windows, and without the pointer), **Take
  over**, **Give back** and Stop. A page it leaves open for you (a sign-in)
  stays after its turn; its Work card in Brain's thread has a **Take over**
  button that opens Screen at that window with the agent held. It goes when its Worker
  closes, when you close it, or after an hour nobody used it, and you are told.
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

### Agents' displays

On a Linux computer, Screen shows one agent's monitor at a time, full size, and
only that one (you are never sent another agent's). The agent's name at the top
opens the list of every agent with a display (on a wide window they are tabs);
a Mac's agent windows are listed the same way, with the Mac's own screen in the
list and in **⋯**. An agent appears as soon as its browser window opens or it
opens an app, whichever tool it used; you never open or manage a browser
yourself. **⋯ › Phone** opens the phone, where Phone use is on.

**Take over from a phone lays the page out for the phone.** When you take over
an agent's browser window, Mewla makes that Chrome window the shape of your
phone and zooms its page to your phone's sharpness, as a person zooming a
desktop window would: the page lays itself out at your phone's width and you
see it at its real size, with a domain chip at the top naming the page's real
site. The zoom stays as you go from page to page: a link, a reload or the next
step of a sign-in. Tap fields, type with your keyboard, paste, and **Give back**: the window,
its zoom and the agent screen go back as they were, with what you typed, the
page you reached and any captcha you solved still there. The agent's monitor
grows taller while you have it and no other agent's changes. A desktop or a
wide window shows the agent's window as it is. A desktop app is shown as it is
too; zoom in on it.

A page an agent leaves open for you (a sign-in, a code, a captcha, a payment)
stays after its turn ends. Its Work card in Brain's thread then has a
**Screen** button that opens Screen at that agent's view, so you don't have to
look for it. The window and the agent's monitor go when its Worker closes, when
you close the window (Chrome's own tab or window close), or when nobody has
used them for an hour: no action by the agent, nothing from you, and no view of
it open in Screen. Mewla then tells you in a notification, and the agent hears
it with its next tool result. While a page or app is open on a Worker's
display, Mewla does not close that Worker's Work as finished, so the page stays
however the Worker reported its turn.

A computer has six agent displays. When all six are taken and another agent
needs one, the display of a Worker that is done, that nobody is watching and
that you have not touched for ten minutes goes to it (the one idle longest),
and you are told the same way. When every display is in active use, the agent
is told so in words and says so in Chat.

- **The view is the computer.** It opens live and is the input surface: on a
  desktop or in the web app, the mouse (click, double click, right click, drag),
  the wheel and the keyboard (shortcuts included) go straight to the agent's
  monitor, a paste sends your text there, and **⋯ › Full size** fills the window
  and hands the browser's own shortcuts to the screen where it allows. Chrome
  on the screen is the real Chrome with its own address bar and tabs; use them.
  On a phone the gestures and the key bar are Screen's own ([above](#use-it)).
- **The agent is told, not paused.** What you do reaches the screen at once
  while the agent keeps going. It is told what you did (clicks, scrolls, how
  much you typed, never what you typed) and decides for itself whether to go on,
  wait or ask you; nothing stops it by rule, since you may only have tapped by
  mistake or be typing a password.
- **Take over** is the one soft switch here: the agent's next action or browser
  call says you hold the screen, and it waits. **Give back** lets it go on, and
  it hears how long you held it. Leaving Screen, or the app going to the
  background, gives it back by itself. A Work card whose agent left a page open
  for you has **Take over**, which opens Screen with the agent already held.

With **Settings › Agents › Virtual screen** on (tap the cat, then **Settings**), a
Worker (Claude or Codex) works in the agents' browser on the virtual screen
instead of your Chrome. It is the same browser tool every Worker has,
`mewla_chrome`, with the same Mewla extension in that Chrome, so nothing about
the tool differs from a Mac's. The browser comes up when the agent first asks
for it, not when the Worker starts, and the agent waits for it. It is one
Chrome with one profile; every agent has a window of its own in it, placed on
that agent's monitor (so are the windows its pages open, such as a sign-in
pop-up), and several agents work at the same time. The window stays after the
agent's turn ends and closes as described above; the browser stops when no
agent has a window. Restarting or updating Mewla keeps the browser, every
agent's page in it and the agent screen: they run in processes of their own,
the new Mewla takes them back and lists the same agents in Screen, and agents
keep driving their windows. If Mewla does not come back within ten minutes,
the browser and the screen stop by themselves. Desktop apps an agent opened
end with the old Mewla. The switch lives on the computer, so every device sees
the same answer.

Desktop apps need no switch. A Worker (Claude or Codex) has the same computer
tool as on a Mac (`mewla_computer`): `open_app` starts an installed app (by the
name its menu entry shows, such as KWrite, or its program) on the agent's own
monitor, `app_state` reads the app's buttons, fields and text through AT-SPI,
and `element`, `steps`, screenshots, clicks and keys act there. Screen lists
the agent as soon as it has a monitor, with the same live view and Take over.
The
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
given up when its Worker closes, when nothing is left on it, or after an hour
nobody used it.

Watching an agent's screen also needs `ffmpeg` with an H.264 encoder (VA-API,
x264 or OpenH264), which encodes its video; the agents work without it. A
computer without one says so on Screen, and `mewla doctor` lists what Screen
is missing (`Xvnc`, `xauth`, `ffmpeg`, an H.264 encoder) on its **Screen**
line.

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
Mac's screen, or the phone (see [Phone use](phone.md)). It is one window for
all three. On a page that belongs to no Session or Work
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
- **It is for watching.** A tap on the picture opens Screen at that agent;
  **Take over** in its menu opens Screen with the agent already held (on a
  Linux display or a Mac's agent window), and **Stop** stops it. The phone's
  window works the same way with Phone use.

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

Open **⋯ › Connection**. The route says whether the video reaches you through
Mewla Relay or directly, and the slowest stage, when it isn't the network,
says what holds the picture up.

- **Network** is high and the route is **Relay**: the round trip through the
  relay dominates, and it is set by where the relay is: a relay on another
  continent adds its distance twice (phone to relay, relay to computer). Input
  then reaches the screen that much later too; only a nearer relay or a direct
  path shortens that. A direct path (Tailscale, or Wi-Fi at home) avoids it,
  and the video takes one by itself when the phone can reach the computer's
  address on that network. If the route stays **Relay** at home, the phone
  can't reach the computer's private address: another Wi-Fi network, client
  isolation on the router, or a firewall. When the computer listed private
  addresses and none could be verified from this phone, Connection says "Join
  your computer's Wi-Fi or Tailscale" (turn Tailscale on on the phone, or join
  the same Wi-Fi). When the computer lists none, the phone can't change that and
  the details give no fix.
- **Slowest** reads **encode**: the computer is busy; a picture normally takes
  2 to 10 ms to encode.

At the computer, one line per session in Mewla's log sums it up (`screen:
device … session ended …`): how long it lasted, how the viewer arrived, the
pictures and key frames sent, the bytes, the encoder and the route. It holds
numbers only.
