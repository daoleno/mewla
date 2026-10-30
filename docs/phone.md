# Phone use

Agents on your computer can use your Android phone: open an app, read what's on
the screen, tap, type and scroll, the way you would. They work on a hidden
screen of their own, so you keep using the phone. The picture and every action
travel over the same signed connection as everything else; nothing is opened on
the phone's network and no account is needed.

Phone use is in **Labs**: off until you turn it on. It is for Android 11 or
later, in the APK from GitHub (the Play Store build leaves it out).

## Turn it on

1. **Settings → Labs → Phone use**, switch on. The Phone page opens, and Phone is added to the left menu.
2. The page lists what the phone needs and checks it live: **Developer
   options**, **Wi‑Fi**, **Wireless debugging**, **Pairing** and **Helper**. Tap
   the button for the first one that isn't done. Each opens the right place in
   Settings and the tick appears when you come back.
3. **Pairing** pairs Mewla with the phone's own Wireless debugging, once. With
   **Pair automatically** on (an Accessibility switch for Mewla), Mewla reads
   the six digits off the Settings screen itself. Without it, a notification
   asks for the code: pull it down and type the digits shown on the pairing
   dialog.
4. When every line is ticked the page says **Ready**. There is nothing more to
   start: the helper starts itself, and starts again after a Wi‑Fi change or
   restart.

The computer you're connected to is the one agents come from. Pair your phone
with another computer and switch to it, and Phone use follows. Agents on
[your other computers](computers.md) reach the phone through that computer.

## What you see

While an agent works, the Phone page shows its screen live, and a notification
says who is using your phone.

- **Take over** makes the picture touchable: your finger drives the agent's
  screen. **Give back** returns it.
- **Stop** ends the agent's work at once, from the page, the notification or the
  floating window. The agent is told and stops, and the page and the notification
  offer **Resume** when you want it to work again.
- When an agent needs you (a login, a code, a payment) your phone notifies you
  with what to do. Tap it, do it on the live screen, and tap **Done**; the agent
  carries on.
- Leave Mewla while an agent works and its screen floats in a small window
  (Android's picture-in-picture), still live and showing only the agent's screen;
  tap it for Take over and Stop.

You always come first: while you are driving or after you Stop, agents wait.

## What agents can do

Agents Mewla starts get a `mewla_phone` tool set while Phone use is on:

| Tool | What it does |
|---|---|
| `phone_screen` | Reads the screen as a list of elements with stable numbers, and a picture when asked |
| `phone_apps`, `phone_launch` | Lists the apps and opens one on the agent's own screen |
| `phone_tap`, `phone_swipe`, `phone_type`, `phone_key` | Presses, scrolls, types (any language) and presses Back, Home or Enter |
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
  itself while Phone use is on; an agent that asks in between is told the phone is
  not ready rather than left waiting.
- Apps that block screen capture (banking, some video apps) show a black
  picture, and apps that block automation may refuse. The agent is told.
- **Pair automatically** needs Mewla's Accessibility switch. Android may grey it
  out for an app installed from a file: open **App info → ⋮ → Allow restricted
  settings** for Mewla first.
- iOS has no Phone use.

## Turn it off

Switch **Phone use** off in **Settings → Labs → Phone use**. The helper stops, the notification goes,
the Accessibility service is disabled, and agents lose the phone tools for new
Workers. To forget the pairing, remove Mewla under **Wireless debugging → Paired
devices** in Settings.
