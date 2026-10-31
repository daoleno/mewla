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
   it. That is the last time: the first helper start also grants Mewla the
   permission to switch Wireless debugging back on, so after a restart or a
   Wi‑Fi change the helper and the connection to your computer come back by
   themselves, with Mewla closed.

Without Accessibility the manual path still works: each row opens the right
place in Settings, and a notification asks for the pairing code (pull it down
and type the digits shown on the pairing dialog).

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
- While an agent works, its screen also floats as a live window over every
  other page of Mewla, so you can keep chatting with Brain and watch at once. It
  has the agent screen's exact shape and starts at a bit over half the screen's
  width, in the bottom-right corner above the chat input, so most of the
  conversation stays readable. Until the agent's screen has drawn anything, it shows
  a light card with a spinner (and the app's name, *微信*, once it is opening, or
  *Couldn't open 微信*) instead of a black slab. Drag the handle in its inner corner, or pinch, to resize
  it (from about a third of the screen to nearly all of it); double-tap it to go
  between the default and a large size. Drag it: it snaps to a corner, and it
  remembers its corner and size. It stays above the chat input and the keyboard.
  Tap it for Take over (which opens the Phone page), Stop and expand. When the
  agent needs you, it shows a pulsing ring. It disappears when the agent finishes
  (its Worker ends or is closed) or you Stop, and on the Phone page itself.
- Leave Mewla from any page while an agent works and its screen floats in a small
  window (Android's picture-in-picture), still live and showing only the agent's
  screen; tap it for Take over and Stop. Coming back returns it to the in-app
  float.

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
  helper keeps working, and the **Phone** page offers one **Start**, or, if
  Mewla may not switch Wireless debugging on yet, that one switch. A phone that
  is not ready tells an agent so (*Phone use isn't ready on the phone: open
  Mewla → Phone*, with the reason when it knows it) instead of showing a black
  screen. The agent is told to wait about 15 seconds and try again for up to a
  minute, since the phone is often ready within a minute of the person opening
  Mewla, and to tell the person only if it still is not.
- The hidden screen draws only when something changes. After an action with no new frame, the answer says the picture may be older than the screen. Apps that draw their own UI (WeChat) are given a longer settle and always come with a picture.
- If an app reports none of its views as visible (WeChat on the hidden screen did), the reader trusts each element's position instead, and elements also carry their state description and tooltip as their description.
- Apps that block screen capture (banking, some video apps) show a black
  picture, and apps that block automation may refuse. The agent is told.
- Mewla's Accessibility service only acts while **Set up** runs, only inside
  Settings and the system's prompt, and never answers a system prompt for you.
- On a network Android hasn't been told to allow, switching Wireless debugging on
  raises the system's prompt. Mewla only does that by itself on a network where
  it worked, and stops asking after the prompt is dismissed twice.
- iOS has no Phone use.

## Turn it off

Switch **Phone use** off in **Settings → Labs → Phone use**. The helper stops, the notification goes,
the Accessibility service is disabled, and agents lose the phone tools for new
Workers. To forget the pairing, remove Mewla under **Wireless debugging → Paired
devices** in Settings.
