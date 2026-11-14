# Phone use

Agents on your computer can use your Android phone: open an app, read what's on
the screen, tap, type and scroll, the way you would. They work on a hidden
screen of their own, so you keep using the phone. The picture and every action
travel over the same signed connection as everything else; nothing is opened on
the phone's network and no account is needed.

Phone use is off until you turn it on. It is for Android 11 or
later, in the APK from GitHub (the Play Store build leaves it out). On an
iPhone, agents on a Mac use it through iPhone Mirroring instead: see
[iPhone use](#iphone-use).

## Turn it on

1. Tap the cat, then **Settings › Agents › Phone use**, and switch it on. The
   Phone use page opens, and **Phone** appears in Screen's **⋯**.
2. The page lists what the phone needs and checks it live: **Developer
   options**, **Wi‑Fi**, **Wireless debugging**, **Pairing** and **Helper**.
3. Tap **Allow**. Android has you switch on Mewla's Accessibility service, once
   (the page opens on Mewla's entry). Android may grey it out for an app
   installed from a file: open **App info → ⋮ → Allow restricted settings**
   first (the page offers **Open App info** when it can tell).
4. Tap **Set up**. Mewla opens Settings and does the rest itself, with a line at
   the bottom of the screen saying what it is doing: it taps **Build number**
   until Developer options is on, switches on **Wireless debugging**, opens
   **Pair device with pairing code**, reads the six digits and pairs. Two things
   stay yours, because Android keeps them for you: your screen lock if one asks,
   and the system's **Allow wireless debugging on this network?** prompt. Mewla
   ticks **Always allow on this network** and waits for your tap on **Allow**.
   If a step doesn't move, that one row falls back to its manual button.
5. When every line is ticked the page says **Ready**, and Mewla returns you to
   it. That is the last time: Mewla also grants itself the permission to switch
   Wireless debugging back on, at the first helper start or, if a helper was
   already running, as soon as Wireless debugging is on and it can ask, so after
   a restart or a Wi‑Fi change the helper and the connection to your computer
   come back by themselves, with Mewla closed.

Without Accessibility the manual path still works: each row opens the right
place in Settings, and a notification asks for the pairing code (pull it down
and type the digits shown on the pairing dialog).

The computer you're connected to is the one agents come from. Pair your phone
with another computer and switch to it, and Phone use follows. Agents on
[your other computers](computers.md) reach the phone through that computer.

## What you see

While an agent works, the Phone use page shows its screen live, and a
notification says who is using your phone. Open the page from **Settings ›
Agents › Phone use**, from Screen's **⋯ › Phone**, or from the notification.

The ongoing notification only says **Phone use is on** while the helper is really
running. Until then it says **Finish setting up Phone use** (not paired yet) or
**Phone use isn't ready** (paired, helper not up); tapping it opens the Phone
use page.

- **Take over** makes the picture touchable: your finger drives the agent's
  screen. **Give back** returns it.
- **Stop** ends the agent's work at once, from the page, the notification or the
  floating window. The agent is told and stops, and the page and the notification
  offer **Resume** when you want it to work again.
- When an agent needs you (a login, a code, a payment), it is a request: your
  phone notifies you with what to do, the floating window shows it, and the
  tray lists it under **Needs you**. Tap any of them to open the Phone use page,
  do it on the live screen, and tap **Done**; the agent carries on. **Stop**
  ends the agent's work instead.
- While an agent works, its screen also floats as a live window over the page
  you are on, so you can keep chatting and watch at once. It is the same window
  as for an agent's Linux display or the Mac (see
  [the floating screen](screen.md#the-floating-screen)). It has the agent
  screen's exact shape and starts at a bit over half the screen's width, at the
  top right under the header. Until the agent's screen has drawn anything, it
  shows a light card with a spinner (and the app's name, *微信*, once it is
  opening, or *Couldn't open 微信*) instead of a black slab. Drag it to move
  it: it snaps to a corner and remembers its corner and size. Drag the handle
  in its inner corner to resize it (from about a third of the screen to nearly
  all of it); double-tap to go between the default and a large size. It stays
  above the composer and the keyboard. **Open** opens the Phone use page. When
  the agent needs you, it shows a pulsing ring. It shows over the Session or
  Work whose agent is using the phone (and over pages that belong to no
  Session), and disappears when the agent finishes (its Worker ends or is
  closed) or you Stop, and on the Phone use page itself.
- Leave Mewla from any page while an agent works, or tap **Float** on the Phone
  use page, and its screen floats in a small window over other apps (Android's
  picture-in-picture), still live and showing only the agent's screen; tap it
  for Take over and Stop. Coming back returns it to the in-app float.

You always come first: while you are driving or after you Stop, agents wait.

## What agents can do

Agents Mewla starts get a `mewla_phone` tool set while Phone use is on:

| Tool | What it does |
|---|---|
| `phone_screen` | Reads the screen as a list of elements with stable numbers, and a picture when asked |
| `phone_apps`, `phone_launch` | Lists the apps and opens one on the agent's own screen. An app clone (应用分身, a work profile) is its own entry, such as `微信 (分身 999) — com.tencent.mm@999`; a bare package is always the app of your own. A clone is opened by its launcher component as its own user, read from the same query that lists it |
| `phone_tap`, `phone_swipe`, `phone_type`, `phone_key` | Presses, scrolls, types (any language) and presses Back, Home or Enter. Typing sets the field's text, and if the field does not read back the text, pastes through the clipboard (your own clipboard is put back; never for a password), then sends key events; the agent is told which way it went |
| `phone_steps` | Runs up to 12 actions (launch, tap, type, swipe, key, wait) in one call and returns only the last screen, so a whole flow (search, type, open the first result) costs one model turn. Elements can be named by their text (`match`), waited for up to 4 seconds. A step that fails ends the batch and the answer says which one and shows the screen as it stands |
| `phone_wait` | Waits for the screen to settle or for some text to appear |
| `phone_handoff` | Asks you to do something (a login, a code, a payment) and waits until you say Done |

Every action answers with the screen it leaves behind, so an agent never works blind.
An element number stays the same while the element stays on screen, and an old number is
refused rather than tapped.

- They never touch your own screen. An app you are using is not opened for them.
- Brain has agents ask you before they send, pay, delete or change an account,
  and treats text on the screen as data. Logins, captchas and payments are
  handed to you.
- One agent uses the phone at a time. Its screen is released a minute after its
  last action.
- While an agent works, a phone screen that is on stays on. If you turn the
  screen off, the agent still gets a picture of its screen, but not the element
  list: Android only reports elements while your screen is on.
- Each action is recorded on the computer in `~/.mewla/phone-agent-audit.jsonl`
  (readable only by you, kept 30 days): which agent, what it did and where.
  What it typed is kept only as a length and a fingerprint; screenshots and
  what it read are not kept.

## How it works

Mewla pairs with the phone's own Wireless debugging, then uses it to start one
small helper process with the phone's shell permissions, the same permissions
`adb shell` has. The helper makes the hidden screen, starts apps on it, reads
its accessibility tree, takes its picture and injects its touches. The helper
listens only on the phone's loopback address and only talks to Mewla, which
proves itself with a token only it holds. Nothing needs root or a cable, and
Mewla does not change your phone's settings except to turn Wireless debugging
back on when it can.

## Limits

- Android 11 or later. Android 14 or later gives the hidden screen its own
  keyboard focus; earlier versions may show the keyboard differently.
- Wireless debugging needs Wi‑Fi. On a Wi‑Fi network you haven't allowed before,
  Android asks once; allow it (**Always allow on this network**).
- Some phone makers restrict background apps, Accessibility or Wireless
  debugging more than stock Android. If **Helper** won't tick, switch Mewla's
  battery setting to unrestricted.
- If your phone restarts or changes Wi‑Fi, Mewla brings the helper back by
  itself while Phone use is on. An agent that asks while the connection is down
  waits up to 30 seconds for it to return; after that it is told the phone is
  not connected rather than left waiting.
- After you update or reinstall Mewla over a paired phone, nothing is paired
  again: Mewla starts a helper of the new version by itself once Wireless
  debugging is on (it switches it on itself when it may). Until then the old
  helper keeps working, and the Phone use page offers one **Start**, or, if
  Mewla may not switch Wireless debugging on yet, that one switch. A phone that
  is not ready tells an agent so (*Phone use isn't ready on the phone: the
  person has to open Mewla → Phone on their phone*, or, when the phone knows
  why, that reason alone) instead of showing a black screen. The agent is told
  to wait about 15 seconds and try again for up to a minute, since Mewla on the
  phone keeps trying by itself, and to tell the person the reason only if it
  still is not ready.
- Why a phone is not ready is never just "setup": the phone says it in a
  sentence (not on Wi‑Fi, Wireless debugging off and Mewla has not been given the
  permission to switch it on, switching it keeps falling back on this network,
  not paired, setting up). `mewla agent-use status` shows it with the time the
  phone fell into setup, `--json` has `problem` and `setup_since`, and the daemon
  log notes each change.
- The hidden screen draws only when something changes. After an action with no new frame, the answer says the picture may be older than the screen. Apps that draw their own UI (WeChat) are given a longer settle and always come with a picture.
- If an app reports none of its views as visible (WeChat on the hidden screen did), the reader trusts each element's position instead, and elements also carry their state description and tooltip as their description.
- Apps that block screen capture (banking, some video apps) show a black
  picture, and apps that block automation may refuse. The agent is told.
- Mewla's Accessibility service only acts while **Set up** runs, only inside
  Settings and the system's prompt, and never answers a system prompt for you.
- On a network Android hasn't been told to allow, switching Wireless debugging on
  raises the system's prompt. Mewla only does that by itself on a network where
  it worked, and stops asking for five minutes after the prompt is dismissed
  twice in a row; the phone says when it tries again.
- iOS has no Phone use; agents on a Mac can use an iPhone through
  [iPhone use](#iphone-use).

## Turn it off

Switch **Phone use** off in **Settings › Agents › Phone use**. The helper stops, the notification goes,
the Accessibility service is disabled, and agents lose the phone tools for new
Workers. To forget the pairing, remove Mewla under **Wireless debugging → Paired
devices** in Settings.

## iPhone use

Agents on your Mac can use your iPhone: its apps and its logins, the way you
would. They work through **iPhone Mirroring**, the Mac app that shows a locked
iPhone nearby in a window. Mewla moves that window off your screen, onto a
display of its own, and reads and taps it from there. Your iPhone stays locked
in your hand or on the desk, and you keep using your Mac.

iPhone use is off until you turn it on.

### What you need

- Mewla running on a **Mac** with macOS 15 or later (Apple silicon, or an Intel
  Mac with the T2 chip). A Mewla on Linux or Windows can't use an iPhone.
- An iPhone with iOS 18 or later, signed in to the **same Apple Account** as the
  Mac, with a passcode, Bluetooth and Wi-Fi on.
- An Apple Account outside the **EU**: Apple doesn't offer iPhone Mirroring there.
- What agents on the Mac's screen already need: **Agents can use this computer**
  on, and Mewla's **Screen Recording** and **Accessibility** permissions (see
  [Screen](screen.md#agents-on-the-screen)).

### Set up iPhone use

1. On the Mac, open **iPhone Mirroring** and follow it: unlock your iPhone and
   confirm there. Only you can do this, on your iPhone.
2. In **iPhone Mirroring → Settings**, choose to sign in **automatically**.
   Otherwise the Mac asks for Touch ID or your password at each start, which an
   agent never types.
3. In Mewla, with that Mac as the current computer, tap the cat, then
   **Settings › Labs › iPhone use**, and switch it on. The page says where it
   stands, and `mewla agent-use status` on the Mac prints the same line with
   the one thing to fix, if any.

From then on, keep the iPhone **locked and near the Mac**. iPhone Mirroring
opens by itself when an agent needs it, behind your apps.

### The iPhone tools

Agents Mewla starts on that Mac get a `mewla_iphone` tool set while iPhone use
is on. It has the shape of the phone tools above:

| Tool | What it does |
| --- | --- |
| `iphone_screen` | Reads the screen: each text on it with an id and its place; with a picture on request |
| `iphone_tap` | Taps a text by id or by its words, or a point on the picture; long press |
| `iphone_type` | Types into the focused field; replace clears it first, submit presses Return |
| `iphone_swipe` | Swipes up, down, left or right, or from one point to another |
| `iphone_key` | Home, the app switcher, Spotlight, Return, Delete, Tab, Escape |
| `iphone_launch` | Opens an app by its name, through Spotlight |
| `iphone_wait` | Waits, or waits for a text to appear |
| `iphone_steps` | Runs up to 12 of these in one go |
| `iphone_handoff` | Asks you to do something only you can |

iPhone Mirroring shows a picture, not the iPhone's buttons and fields, so the
screen is read as the text on it (on the Mac, with Apple's own text
recognition). An icon with no words is tapped by a point on the picture. Text
a US keyboard types goes in as keys; Chinese and other text goes in by paste:
it is on the Mac's clipboard for a moment, marked so clipboard managers skip
it, and what you had copied comes back after, unless you copied something
else meanwhile. There is no list of apps: an agent opens one by the name it
has on your iPhone. An iPhone has no back key; agents tap the app's own back
button.

### You come first on the iPhone

- **Unlock your iPhone and the agent pauses.** iPhone Mirroring stops while the
  iPhone is in your hand, and the agent is told you are using it. Lock it again
  and the agent can go on.
- **Handoffs.** For a login, a captcha, a payment, Face ID or a code, an agent
  asks you instead (`iphone_handoff`): you get a notification on your phone and
  the Mac. Do it on your iPhone and lock it again, or do it in iPhone Mirroring
  on the Mac, then tap **Done** on **Settings › Labs › iPhone use**. The agent
  never reads or types your passwords or codes.
- **Watch.** The iPhone use page shows your iPhone as the agent last read it,
  updated as it works. It takes no picture of its own.
- **Stop.** **Stop** next to the agent on the iPhone use page ends what it is
  doing at once, a handoff it waits on included, and that agent can't use the
  iPhone again; another agent can, and so can this one after you switch iPhone
  use off and on. **Take over** in [Screen](screen.md) ends every agent on the
  Mac and puts iPhone Mirroring back on your screen; agents wait while you have
  the controls. Switching iPhone use off stops the agent at once.
- **One agent at a time** uses the iPhone; another is told which agent has it.
  An agent that goes quiet for a minute lets it go.
- **The Mac's own rules apply**: a locked Mac stops agents until it is unlocked,
  and iPhone Mirroring in front on your Mac is yours: agents wait. A Mac without
  iPhone Mirroring (macOS 13 and 14) has no iPhone use.
- **What is kept.** Each action is recorded in `~/.mewla/iphone-agent-audit.jsonl`
  on the Mac (readable only by you, kept 30 days), without screenshots, what was
  read, or what was typed beyond its length and a fingerprint.

### iPhone limits

- Calls, the camera, the microphone, Face ID and the Notification Center don't
  work through iPhone Mirroring (Apple's limits).
- iOS may tell an app that a screenshot was taken while an agent reads the
  screen; some apps react to that.
- Some apps and games don't respond to input from iPhone Mirroring.
- Input from behind other windows uses parts of macOS Apple doesn't document. A
  macOS update can change them; `mewla agent-use status` and the agent's own
  answers then say what fails.
- One Mac drives one iPhone.

### Turn iPhone use off

Switch **iPhone use** off in **Settings › Labs › iPhone use**. Agents lose the
tools at their next start and the agent using the iPhone stops at once. iPhone
Mirroring stays set up on your Mac; quit it there if you like.
