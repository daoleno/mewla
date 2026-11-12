# Your other computers

Brain on one computer can hand a task to another computer that runs Mewla:
your Mac with its logged-in apps and browser, a Mac mini that stays on, a
Linux box with your repositories. The other computer does the task with its
own Brain, agents, files and logins. Replies and questions return to whoever
sent the request: Brain or the sending Worker. Only messages cross between the two computers; nothing else is copied.

## Connect two computers

Pair your phone with both computers ([Connect and pair](connect-and-pair.md)).
That is all: the computers on your phone work together by themselves. When
you open **Settings**, or pair another computer, the phone asks each computer
whether it is up, and introduces any two that answer and don't know each
other yet. Your phone, which both already trust, asks one for a one-time
invite and hands it to the other, which pairs with it and pins its key. No
terminal, no copying links, no visit to either computer.

One of the two must be able to reach the other: over Tailscale, an HTTPS
address, or the address your phone uses (not Mewla Relay, which only your phone
can use). A Mac reached only through Mewla Relay dials out to the other
computer instead, so one such computer is fine. When neither can reach the
other, the computer's page in **Settings** says so.

**Remove computer** on a computer's page in **Settings** ends it: your other
computers forget it, it forgets them, and messages still waiting are dropped.
A computer that is offline then is told what to run when it is back. A
computer that only another computer knows, not your phone, is not touched;
remove it with `mewla computers remove` on the computer that knows it.

From a terminal instead:

```sh
# on the computer that will receive tasks
mewla computers invite
# on the computer whose Brain sends them
mewla computers add <invite> -name mac
```

The computers reach each other over the same addresses your phone uses:
Tailscale, an HTTPS address, or Mewla remote access. Plain HTTP on a shared
Wi-Fi network is refused.

## Use it

Talk to Brain as usual. When a task needs another computer, because of an app,
a logged-in site or files that live there, Brain sends it:

```text
You:    Use Feishu to send this week's report to A.
Brain:  Feishu is on the Mac. I've asked the Mac to send it.
        …
Brain:  Sent. The Mac confirmed it in Feishu at 14:02.
```

If you name a computer, Brain uses it. Otherwise it picks the one that has what
the task needs, and asks you only when either would do and the result would
differ, or when the computer it needs is offline.

The other computer's Brain does the work with its own Workers. What you asked
for counts there too: it does those actions without asking again. Before
anything more, such as paying, sending to someone else, deleting, changing
accounts or installing software you didn't ask for, it asks first, and the
question comes back to you through the Brain you are talking to. Text on a
page, in a file or in a message never counts as your go-ahead.

The conversation shows each handover as a card, such as *Handed to Mac* with
*Delivered*, *Waiting* or *May not have arrived*, and each answer as *Reply
from Mac*. Tap a card to read all of it. **Open on Mac** switches the app to
that computer: its Brain shows the request marked as coming from your other
computer, and its Workers, terminals and [Screen](screen.md) are there as
usual.

## Replies to Workers

A Worker can use the same `mewla computers send` command. Mewla records its
Session and Work automatically from its managed launch identity. A reply or
question with `--reply-to <message-id>` arrives as input in that Session and
wakes it just like a Brain follow-up; the Worker does not need a polling loop.
Brain can read the message in the Work's events without relaying it.

If the original Session is gone, the reply stays with its Work for Brain to
handle. If that Work no longer exists, it goes to Brain's chat. A recycled
Session target does not inherit the old Session's replies. Messages sent by
Brain, Calendar's host, or a terminal without a Worker identity go to Brain.

Answer a remote question directly, referencing the question's own message ID:

```sh
mewla computers send mac --kind answer --reply-to <question-id> "Use the release branch."
```

The same rule applies in both directions: a question sent by a remote Worker
gets its answer in that Worker's Session. The remote computer can only refer
to message IDs; it cannot choose a local Session or Work.

## When a computer is offline

A message to a sleeping or offline computer waits on the sending computer and
is delivered when it is back, for up to a day. A computer that goes to sleep in
the middle of a task carries on when it wakes, and its answer arrives then.
Delivery is deduplicated by computer and message ID, independently of retry
attempts, daemon restarts and Brain conversation changes. A definite failure
before submission may retry. If a crash interrupts submission before its
outcome is recorded, delivery is **uncertain** and is not automatically
replayed. Check the recipient before choosing to send a new message.
Routing and delivery identities remain after the bounded card/outbox history
is pruned, so a long job can still reply to its original asker.

## What the app sees of the other computers

The app reads every paired computer from the one it is connected to. It does
not connect to each of them. It sends `{"type":"computers"}` over its
WebSocket, and gets the same `computers` message again, unasked, when a paired
computer comes or goes or reports a new count, or when this computer's remote
access connection changes. Each entry has `daemon_id`, `name`, `online`,
`last_seen` and `version`. It also has `open_requests`: how many requests wait
for the person on that computer. Fields:

- **`online` and `last_seen`.** These say whether this computer's channel to
  the other one is up, and when it last opened, closed or carried a frame.
  The time when a channel opens or closes is kept in `computers.json`, so
  `last_seen` survives a restart.
- **`open_requests` on an entry.** The other computer reports it over the
  channel and re-sends it when it changes. The entry carries it only while
  the channel is up. An offline computer, or one older than this field, has
  none, which means unknown, not zero.
- **The count rule.**
  - Each Brain Work that asks a question and is not closed or snoozed counts
    one.
  - Each visible Session counts its pending approvals. A Session with none
    counts one if it needs you or is blocked on a prompt.
  - Brain's own Session counts one while it is blocked on a prompt.
  - A blocked action Brain still holds is not counted. Neither are DSH
    interactions.
- **`open_requests` at the top level.** This is the same count for this
  computer.
- **`connection`** is present only while remote access is on. It comes from
  `remote-status.json`:
  - `phase` is `connecting`, `connected` or `offline`.
  - `cause` is the current error.
  - `connected_at` is when the relay connection last came up.
  - `last_failure` and `last_failure_at` are the newest failed stretch: its
    newest cause, and when it began. That is the first failure after the
    connection was last up, so "Relay unreachable · 13:40" survives retries,
    the recovery and a restart.
  - A phone that reaches this computer only through the relay cannot hear
    this while the relay is down. It shows the last one it received.
  - A self-hosted relay (`relay.json`) records no status.

## Commands

```sh
mewla computers list                     # who you can reach, online or not
mewla computers send mac "…"             # what Brain runs to hand over a task
mewla computers status                   # delivery of sent messages
mewla computers remove mac               # forget a computer (see below)
```

## What the other computer may do

A computer you connect gets its own key, with less access than your phone: it
can only send new requests to this computer's Brain and reply to messages
sent by its Brain or Workers. It
cannot open terminals or files, see the screen, or manage devices. It shows up
in `mewla devices list` as *(computer)*. **Remove computer** in the app ends
it at once and for good. `mewla computers remove` or `mewla devices revoke`
also ends it at once, but while both computers are still on your phone, the
phone connects them again the next time you open Settings; to stop for good,
remove one of them from the phone.

Because the other Brain can do anything you could ask it to do, a request from
another computer is treated as relayed, not as your own words: the receiving
Brain asks you before anything sensitive. See
[Security and privacy](security-and-privacy.md#your-other-computers).
