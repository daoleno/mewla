# iPhone use

Agents on your Mac can use your iPhone: its apps and its logins, the way you
would. They work through **iPhone Mirroring**, the Mac app that shows a locked
iPhone nearby in a window. Mewla moves that window off your screen, onto a
display of its own, and reads and taps it from there. Your iPhone stays locked
in your hand or on the desk, and you keep using your Mac.

iPhone use is in **Labs**: off until you turn it on.

## What you need

- Mewla running on a **Mac** with macOS 15 or later (Apple silicon, or an Intel
  Mac with the T2 chip). A Mewla on Linux or Windows can't use an iPhone.
- An iPhone with iOS 18 or later, signed in to the **same Apple Account** as the
  Mac, with a passcode, Bluetooth and Wi-Fi on.
- An Apple Account outside the **EU**: Apple doesn't offer iPhone Mirroring there.
- What agents on the Mac's screen already need: **Agents can use this computer**
  on, and Mewla's **Screen Recording** and **Accessibility** permissions (see
  [Screen](screen.md)).

## Set it up once

1. On the Mac, open **iPhone Mirroring** and follow it: unlock your iPhone and
   confirm there. Only you can do this, on your iPhone.
2. In **iPhone Mirroring → Settings**, choose to sign in **automatically**.
   Otherwise the Mac asks for Touch ID or your password at each start, which an
   agent never types.
3. In Mewla, with that Mac as the current computer, switch on **Settings → Labs
   → iPhone use**. The page says where it stands, and
   `mewla agent-use status` on the Mac prints the same line with the one thing
   to fix, if any.

From then on, keep the iPhone **locked and near the Mac**. iPhone Mirroring
opens by itself when an agent needs it, behind your apps.

## What agents can do

Agents Mewla starts on that Mac get a `mewla_iphone` tool set while iPhone use
is on. It has the shape of [Phone use](phone.md):

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
a US keyboard types goes in as keys; Chinese and other text goes in by paste,
and the Mac's clipboard is put back after. There is no list of apps: an agent
opens one by the name it has on your iPhone. An iPhone has no back key; agents
tap the app's own back button.

## You come first

- **Unlock your iPhone and the agent pauses.** iPhone Mirroring stops while the
  iPhone is in your hand, and the agent is told you are using it. Lock it again
  and the agent can go on.
- **Handoffs.** For a login, a captcha, a payment, Face ID or a code, an agent
  asks you instead (`iphone_handoff`): you get a notification on your phone and
  the Mac. Do it on your iPhone and lock it again, or do it in iPhone Mirroring
  on the Mac, then tap **Done** in Mewla (Screen, or Settings → Labs → iPhone
  use). The agent never reads or types your passwords or codes.
- **Stop.** **Stop** next to the agent in Screen or in the iPhone use page ends
  it. **Take over** in Screen ends every agent on the Mac and puts iPhone
  Mirroring back on your screen. Switching iPhone use off stops the agent at once.
- **One agent at a time** uses the iPhone; another is told which agent has it.
  An agent that goes quiet for a minute lets it go.
- **The Mac's own rules apply**: a locked Mac stops agents until it is unlocked,
  and iPhone Mirroring in front on your Mac is yours: agents wait.
- **What is kept.** Each action is recorded in `~/.mewla/iphone-agent-audit.jsonl`
  on the Mac (readable only by you, kept 30 days), without screenshots, what was
  read, or what was typed beyond its length and a fingerprint.

## Limits

- Calls, the camera, the microphone, Face ID and the Notification Center don't
  work through iPhone Mirroring (Apple's limits).
- iOS may tell an app that a screenshot was taken while an agent reads the
  screen; some apps react to that.
- Some apps and games don't respond to input from iPhone Mirroring.
- Input from behind other windows uses parts of macOS Apple doesn't document. A
  macOS update can change them; `mewla agent-use status` and the agent's own
  answers then say what fails.
- One Mac drives one iPhone.

## Turn it off

Switch **iPhone use** off in **Settings → Labs → iPhone use**. Agents lose the
tools at their next start and the agent using the iPhone stops at once. iPhone
Mirroring stays set up on your Mac; quit it there if you like.
