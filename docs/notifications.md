# Notifications and Telegram

Mewla interrupts you only when an agent needs you or finished while you were
away. It is not a progress feed. A running task and a question that needs you
can also stay on the phone as a [live notification and widgets](#live-notification-and-widgets).

## Push notifications

The daemon sends a push for three agent states:

| State | Title | Priority |
| --- | --- | --- |
| Needs input | `<name> needs input` | High |
| Failed | `<name> failed` | High |
| Finished | `<name> finished` | Default |

The name is the Session's alias or project, not a raw terminal name. "Finished"
means the Session ended, not that the task succeeded; open it to check.

Mewla stays silent while an agent is running, while its state is unknown, and for
connection changes.

If you are looking at that exact Session in the app, Mewla does not notify you.
Anywhere else, including another Session, it sends the push, with two
exceptions for **Finished**: it is not sent while the app is open in front of
you, and a Work that finishes again in the same run (for example a series of
releases) notifies once. **Needs input** and **Failed** send once per question:
the same prompt blinking, re-rendering or coming back after a daemon restart
does not send again, while a new prompt, or the same one after the Session had
moved on for 30 seconds, does. A newer push for a Session replaces the older
one on your phone instead of stacking. A Work that needed you starts its next
run fresh. Every push held back this way is still recorded in
[Watch](watch.md), with the reason.

Calendar sends two other kinds of alert:

- **Reminders** are scheduled on the phone itself, so they fire without a
  connection to the daemon.
- A **Mewla action** sends one push when its result is ready, which opens
  the Brain thread that holds the result. The alert never contains the result
  itself.

### What a notification opens

A notification opens the thing it is about, with the app built around it so
Back has somewhere to go:

| About | Opens |
| --- | --- |
| A Session | That Session (at its request card, when it asks something) |
| Sessions that need you | Home, with the tray open |
| A Work | Work detail |
| A Calendar item | The item in Calendar |
| An earlier Brain thread | That thread, read-only, with **Current chat** to return |
| Another paired computer | Switches to that computer first |

A question in a notification is the same request as the card in the app and
the **Needs you** row in the tray: answer it from the notification and the card
and the row close too. If the answer cannot be sent, the app says "Couldn't
send that answer".

### What Watch keeps

[Watch](watch.md) keeps a record of every push and every push it held back,
and what you did with it (tapped it, opened that Session, brought the app
forward). When Watch is on, an agent reads them in context and flags kinds of
push that are noise, badly timed, repeated, or that never came when you were
needed. The app tells your computer only the push's id, that you tapped it and
when, and whether the app is in the foreground.

### Delivery

Each push is a single attempt. If delivery fails or the daemon restarts at the
wrong moment, there may be no alert, but the state and results are still there
when you open the app.

Official builds need no setup: install the app, pair, and allow notifications
when the app asks. It asks once, after your first computer connects, not at
launch. If you decline, Mewla does not ask again; turn notifications on in the
system settings for Mewla. The Calendar page shows a notice while they are off.

Your daemon sends each push to Expo's push service with your phone's push
token, and Expo hands it to Google (Android) or Apple (iOS). Every paired
device that has connected since the daemon started gets the push, each under
its own token. The push carries the title, the Session name and a short line,
never the full result. If Expo reports that a phone is no longer registered, the
daemon forgets that token until the app connects again; revoking a device
forgets its token at once. Tokens are kept in memory only, so a phone hears
pushes again after a daemon restart once its app connects.

A fork that signs its own build needs its own Expo project and Firebase app:
set `EXPO_PROJECT_ID` in `client/core/push/registration.ts`, replace
`app/google-services.json` with the file for your Android package, and upload
your FCM V1 key to that Expo project. Without them a fork still works, just
without remote push.

## Live notification and widgets

While Workers run, or while a question waits for you, the phone can show it
without opening the app:

- **Android**: one ongoing notification. On Android 16 QPR1 and later, with
  **Live updates** allowed for Mewla, it is promoted to a status-bar chip (the
  elapsed time, or how many questions are open) and a pinned card with a bar
  for how much of the goal is done. Otherwise it is a plain ongoing
  notification with the same content. Home-screen widgets come in 2×2 and 4×2.
- **iOS**: a Live Activity in the Dynamic Island and on the Lock Screen, with
  the Work's title and phase, a running clock and the same bar. Home-screen
  widgets come in small and medium, and need iOS 17.

What they show:

- **Running**: one Worker shows its Work's title and phase; several collapse to
  "N workers". The clock counts from the earliest start.
- **Needs you**: the question, with up to three of its choices. Tapping a
  choice opens the app on that question and sends the answer once. A question
  outranks running work.
- Nothing running and nothing waiting: the notification and the Live Activity
  go away.

A notification or Live Activity you dismiss stays away until a new Worker
starts or a new question arrives. Everything comes from the current computer
only.

### How it works

The daemon decides what these show, from the same Brain state the app already
receives; the phone only draws it. A change reaches the phone over the app's
connection, or, with the app closed, as a silent push that wakes it. On iOS
that is the only route: Expo's push service cannot send an ActivityKit push. A
late state never overwrites a newer one.

A widget cannot hold the device's key and must not do anything but read. It
gets its own credential from the daemon: read-only, one per device, and refused
once the device is revoked.

### Limits

- **iOS**: a Live Activity can start only while the app is open, so the first
  card of a run appears once you open Mewla. With the app closed, updates come
  by silent push, which iOS throttles and does not deliver after you force-quit
  the app, so an update can be late or missing until you open it again.
- **Widgets on a plain address** read the daemon themselves, and a revoked
  phone stops at the next refresh. **On Mewla Relay** the widget shows only what
  the app and pushes gave it, and says "Open Mewla" once its last state is more
  than 12 hours old. Revoking that phone stops the pushes, but the widget keeps
  its last state until then.
- Live updates are not receipt-tracked: a newer state always follows, and a lost
  one costs nothing.
- A question is answered only if it is still the current one; a late tap changes
  nothing.

## Telegram

Telegram is a second way to talk to the current computer's Brain and Workers.
It uses a Telegram bot that you create; messages go to the same Brain thread as
the app.

### Connect

1. Create a bot with Telegram's [BotFather](https://t.me/BotFather) and copy
   its token.
2. In the app, tap the cat, then **Settings › Telegram**. Paste the token and
   tap **Verify token**.
3. Tap **Connect Telegram**. It opens a short-lived link to your bot; tap
   **Start** in the bot chat. Mewla binds to your Telegram account and private
   chat, not to a username.

**Disconnect** pauses delivery and keeps the binding; **Reconnect** resumes it.
Messages sent while disconnected are not delivered later. **Advanced** holds
token replacement, unlinking your account and removing the bot.

From the computer, `mewla telegram setup`, `status`, `enable` and `disable`
configure the running daemon. They never take a token as a command-line
argument.

### Talk to Brain and Sessions

Plain messages go to Brain. If your bot chat has topics enabled, Mewla creates a
**Brain** topic and one topic per Worker Session; writing in a Session's topic
sends to that Session. When a Session is gone for good, its topic and its
messages are deleted. Pin the Brain topic yourself in Telegram if you want it
on top.

| Command | Effect |
| --- | --- |
| `/sessions` | List current Worker Sessions |
| `/use <number or session id>` | Send following messages to that Session (chats without topics) |
| `/brain` | Go back to Brain |
| `/status` | Show the current Session, turn and Work state |
| `/new` | Start a fresh Brain thread (in a Session topic it does not reset Brain) |
| `/help` | Show the command list |

A selected Session that disappears stays selected and unavailable, so your next
message never silently goes to Brain instead.

### Files

Photos, documents, voice notes and stickers you send reach Brain or the
selected Session as attachments, the same way as uploads from the app. Limits:
20 MiB per file or album, ten files per album. Mewla does not transcribe audio or
interpret video; what the agent can do with a file depends on the agent. Mewla
replies "Files received by Mewla" once the files are accepted.

Replies from Brain and Sessions arrive as text. Mewla does not send files back to
Telegram.

### What Mewla remembers about delivered messages

To edit a message as its source changes and not send it twice, Mewla remembers
which Telegram message each Brain answer, Work card and Session reply became.
It keeps that for the newest 500 messages of the Brain thread and of each
Session, and for anything younger than 14 days. A message outside both limits is
no longer projected to Telegram and its record is deleted, so an agent that was
silent for two weeks and then answers sends a new message instead of editing an
old one. A delivery that is still waiting, in flight or of unknown outcome keeps
its records until it is settled. The limits are `projectionKeepNewest` and
`projectionKeepFor` in `daemon/telegram/retention.go`.

### What Telegram can see

Bot chats are Telegram cloud chats, so Telegram stores their history. Removing
Mewla's configuration does not delete that history. Only your own account in the
bound private chat can send input; groups and other users are ignored.
