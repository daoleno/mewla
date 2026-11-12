# Calendar

Calendar is Mewla's view of time: what happens when. It lives on the daemon
and syncs to the app, so it belongs to the current computer like everything
else.

## Open it

Open the tray (the state control next to **Brain**). **Today** shows the next
three items; tap one to open it. Tap **Calendar** for the whole page:

- **Today** lists the day, with past items dimmed above the line for now.
  **Month** shows the month grid; pick a day to see it.
- **+** adds an item.
- Tap an item to open it. A running item opens its live Session instead.

## Item kinds

| Kind | Has | What happens when it is due |
| --- | --- | --- |
| **Event** | A start and an end | Shows as running during the interval, then completes |
| **Reminder** | A notify time | Your phone shows a notification and the item waits for you |
| **Deadline** | A due time | The item waits for you |
| **Mewla action** | A due time and an instruction | An agent runs the instruction as Work and reports back to Brain |

An item that waits for you shows **Needs you**, counts in the state control and
has a **Needs you** row in the tray. Each row says what the item does in one
line.

Open an item to see:

- its schedule in words ("Every day at 8:30 PM · Next: tomorrow 8:30 PM"),
  with the timezone only when it differs from your phone's;
- what it does, in one line; a Mewla action's full instructions are under
  **Instructions**;
- its runs, newest first: the date, how it went and the first line of what it
  produced. Tap a run for its whole result, or why it failed, with **Open
  session** and **Open its Work**.

**Edit** changes an item; **Run now** (or **Retry now** after a failure) runs a
Mewla action on demand.

You enter a local date, a local time and a timezone. Items can repeat daily,
weekly or on weekdays, and keep their local time across daylight-saving
changes. A time that does not exist on a daylight-saving day is rejected; a
time that occurs twice asks you to choose the first or second occurrence.

## Mewla actions

A Mewla action is a small job for an agent: "At 09:30 on weekdays, summarise
yesterday's merged pull requests." When it is due, Mewla:

1. creates a visible Work item and starts the agent in its own Session, on
   Brain's executor;
2. waits for that Work to finish or fail;
3. posts the result to the Brain thread where you created the action, and
   sends one push notification that opens that thread.

The Work and the Session it starts say where they came from: the Session's row
has a calendar badge and its header names the item and the date ("运营：挖稿 ·
Oct 27"), and Work detail links the item.

Each occurrence runs at most once, even across daemon restarts. A recurring
action moves on to its next time after a success or a failure; the failed run
keeps the reason, and the item itself is simply scheduled again. If the daemon
was down when an action was due, it runs on startup only if it is at most 15
minutes late; older ones are marked failed so stale work does not start
unexpectedly.

Mewla actions run unattended; see
[Permission bypass risks](executors.md#permission-bypass-risks).

## Alerts you will not miss

An event, reminder or deadline alerts your phone, with no setup. An event
alerts 10 minutes before it starts, a deadline 30 minutes before it is due and
a reminder at its time. Each item can carry its own lead: ask Brain "remind me
half an hour before" or pass `-lead <minutes>` on the CLI. An alert fires once
per occurrence, also across daemon restarts, and one the daemon was too late for
(more than 15 minutes) is dropped rather than sent as old news.

Two paths carry every alert, and the phone shows it once:

- **The phone's own alarm.** After a sync the app schedules each upcoming alert
  on the phone, so it fires even when the daemon or the network is late.
- **A push from the daemon,** for what the phone has not synced yet, such as an
  item Brain just created while the app was closed.

On Android the alert is an alarm-style notification in its own Calendar channel
(heads-up and sound); with the screen locked it takes over the screen with
Done and Snooze, and opening the app from it asks for your unlock first. On iOS
it is a Time Sensitive notification, which breaks through Focus.

An alert is a request: when it fires with the app open, or you open the app
from it, a card covers the screen with the title, time and notes, and stays
until you act. Until then it is also a **Needs you** row in the tray, which
opens the card again.

- **Done** finishes a reminder or deadline (a repeating one moves to its next
  time) and silences an event's alert.
- **Snooze 5 min** or **Snooze 15 min** alerts again later.
- **Open in Calendar** goes to the item.

Acting on the card anywhere dismisses it everywhere, on every phone and on the
web.

Settings (tap the cat, then **Settings**) has **Strong calendar alerts**, on by
default. Off, alerts are ordinary notifications (no lock-screen takeover on
Android, no Time Sensitive on iOS) and the card still appears in the app. On
Android, when the system has not allowed alerts over the lock screen, **Allow
full-screen alerts** opens the system setting.

If you deny notification permission, Calendar keeps working and its page says
reminders are not enabled.

## Items Mewla acts on

A reminder can belong to a part of Mewla. [Watch](watch.md#reaching-you) creates
one called "Morning brief": a daily reminder that sends Brain the brief when it
is due, instead of showing a notification. You edit it like any reminder: move
the time, change the repeat, cancel it. Cancelling turns the feature off, and
Mewla does not create it again. It has to stay a reminder, and only the daemon
can create items of this kind.

## Ask Brain or use the CLI

Brain creates calendar items when you ask for one explicitly ("remind me
tomorrow at 9"). It does not pull commitments out of every message.

From the computer (a Mewla action is `scheduled_action`):

```sh
mewla calendar list --json
mewla calendar get -id <item-id> --json
mewla calendar create -title "Review plan" -kind reminder \
  -date 2026-07-15 -time 09:30 -timezone Europe/Berlin --json
mewla calendar create -title "Summarise merged PRs" -kind scheduled_action \
  -date 2026-07-15 -time 09:30 -timezone Europe/Berlin -recurrence weekdays \
  -summary "Every weekday morning, yesterday's merged PRs in brief" \
  -instruction "Summarise yesterday's merged pull requests" \
  -source-thread <brain-thread-id> --json
mewla calendar cancel -id <item-id> -revision <revision> --json
mewla calendar run -id <item-id> --json
mewla calendar snooze -id <item-id> -minutes 15 --json
mewla calendar done -id <item-id> --json
```

`-lead <minutes>` on `create` sets how long before an event or deadline the
phone alerts (default 10 for an event, 30 for a deadline).

A Mewla action needs `-source-thread`, the Brain thread that receives its
result. `-summary` is the one line Calendar shows; `-instruction` is the full
prompt the agent gets. Brain writes the summary in your language. An item
without one shows the first sentence of its instruction. Events use
`-end-date` and `-end-time`. `mewla calendar update` takes the complete item as
`-item-json` plus its current `-revision`, so two edits cannot silently
overwrite each other.

## Limits

Calendar does not sync with Apple or Google calendars, and has no attendees,
shared calendars or automatic scheduling.
