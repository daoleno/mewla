# Calendar

Calendar is Mewla's view of time: what happens when. It lives on the daemon
and syncs to the app, so it belongs to the current computer like everything
else.

## Open it

Open the tray (the state control next to **Brain**). **Today** shows the next
three items; tap one to open it. Tap **Calendar** for the whole page (on a keyboard, `G` then `C`):

- **Today** says how many things happen today ("12 today") and lists the day
  on your device's clock: everything due today in time order, repeats
  included, each with how its run went (done with a check, failed in amber
  with why, running), then a line for now, then what is still to come.
  **Tomorrow** and **Later** follow. Items from earlier days fold into one
  **Earlier** row at the end; tap it to see them by day.
- **Month** shows the month grid. A dot marks days with one-off items, amber
  when one failed; repeats get none. Pick a day to see it: a coming day lists
  every repeat that will run, a past day only what is known (one-off items and
  the runs the Calendar keeps), never a repeat drawn in as if it ran.
- **+** adds an item.
- Tap a row to open it: a running one opens its live Session, a finished run
  its result, the rest the item.

Each row is the time, the title and one quiet line: how it went ("Done · 412
matched"), else how it repeats and who acts ("Every day · Brain", "Every day ·
Mewla" for Mewla's own, "Reminder").

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
   the executor, model and reasoning the action names (see below);
2. waits for that Work to finish or fail;
3. posts the result to the Brain thread where you created the action, and
   sends one push notification that opens that thread.

The agent also says how the task went (`run_outcome:` in its Work: succeeded,
failed, or needs you). A run whose check could not run reads **Failed** even
though it wrote a result; a run that did not say reads **Done** and the first
line of its result.

The Work and the Session it starts say where they came from: the Session's row
has a calendar badge and its header names the item and the date ("运营：挖稿 ·
Oct 27"), and Work detail links the item.

Each occurrence runs at most once, even across daemon restarts. A recurring
action moves on to its next time after a success or a failure; the failed run
keeps the reason, and the item itself is simply scheduled again. If the daemon
was down when an action was due, it runs on startup only if it is at most 15
minutes late; older ones are marked failed so stale work does not start
unexpectedly.

If the action's agent is refused by its model provider (a rate limit, an
overload, a dropped network), the run is **held**, not failed:

- While its Session lives, it waits and is continued in that Session once the
  provider answers. The agent sees what it already did, so nothing is redone
  blindly.
- If the Session ends while the run is held, a new agent starts only if the run
  did no work, meaning every request its agent ever made was refused. An agent that had a
  request answered may have acted, so that run fails instead of running twice.
- A held run is made up at most 12 hours after its time, and never after the
  item's next occurrence; past that it fails with "Not run: … refused requests
  (rate limit) from … until …".
- **Run now** (`mewla calendar run`) on a held run replaces it with a fresh run;
  on a failed one it runs it again.

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

## Triggers

A Trigger watches something for you and wakes Brain when it changes. Ask in
chat: "watch the open pull requests on these repositories and review each new
one", "tell me when the nightly build on main goes red". Brain sets it up and
says in one line what it watches, how often and what happens when it fires.

A Trigger runs a small check command on an interval (every 10 minutes unless
you ask otherwise, at least every minute, at most once a day):

- **Nothing happened:** the check prints nothing. Nothing else happens: no
  model runs and nothing is written anywhere but the Trigger's own "last
  checked" time.
- **Something happened:** the check prints the evidence. When that output
  differs from what the previous check printed, the Trigger fires once: Brain
  wakes on the Trigger's Work with the output and does what you asked. The same
  output again is silent; output that goes away and comes back fires again.
- **More changes while Brain is busy with one:** they are gathered and reach
  Brain together in the next firing, once the current one is handled. Nothing
  is dropped.

Triggers belong to the daemon, not to a conversation, so they keep running when
Brain's Session is replaced, when the daemon restarts and after a reboot. A
check that comes late after the computer was off is noted ("not checked for
2 h: Mewla was not running"); missed checks are not replayed.

The watching itself never stops silently:

- **Failing:** a check that keeps failing (3 failed checks in a row over at
  least 30 minutes) tells Brain once with the error, and again once a day while
  it keeps failing.
- **Paused itself:** a Trigger that would wake Brain more than 6 times in an
  hour pauses and tells Brain; its latest output waits until it is resumed.
- **Not handled:** a firing Brain has not picked up after 3 hours (Brain was
  not running, for example) shows as not handled.

Checks run on this computer as you, in the folder Brain chose, with your PATH
as your shell sets it up (so `gh`, mise shims and `~/.local/bin` are found even
when the daemon started with a minimal PATH). On Windows a check runs in
PowerShell 7 when it is installed and in Windows PowerShell otherwise. A check
gets no input, is stopped after its timeout (1 minute by default), and keeps at
most 64 KiB of output. Each Trigger keeps its last 50 firings.

From the computer:

```sh
mewla trigger list --json
mewla trigger create -title "Open PRs" -every 10m -check './poll.sh' \
  -cwd /path/to/folder -instruction "Review each new pull request" \
  -source-thread <brain-thread-id> --json
mewla trigger get -id <trigger-id> --json
mewla trigger check -id <trigger-id>    # check now
mewla trigger pause -id <trigger-id>
mewla trigger resume -id <trigger-id>
mewla trigger delete -id <trigger-id>
```

In the app, Calendar's Today starts with one **Triggers** row: how many are
watching, and a seal dot with "1 needs you" only when one does (no row when the
computer has none). It opens the **Triggers** page in three groups: **Needs
you** (its check keeps failing, or it paused itself), **Noticed today** (how
often, and when it last did) and **Quiet** ("Last noticed Nov 19", "Nothing
noticed yet", "Paused").

Tap a Trigger to open it: how often it checks; when it needs you, why and since
when, with **Check now** and **Pause** (or **Resume**) on that card; what it
noticed, newest first, and whether Brain handled it; what Brain does when it
notices something; **How it checks** (the command, the folder it runs in and
its timeout); and **Check now**, **Pause** or **Resume**, and **Delete**.

A Trigger that needs you is also under **Needs you** in the tray, with Open:
its check keeps failing, or it paused itself. A firing Brain has not handled
for 3 hours says so on the Trigger's page; it is Brain's, not a Needs you row.
This takes no model call. A Trigger you paused yourself stops asking
about a failing check. A Trigger's Work is listed in the tray only while Brain
is handling one of its firings; otherwise its Trigger row is where it lives.

`-work <work-id>` sends the firings to an existing Work instead of a new one;
without it the Trigger creates its own Work in `-source-thread` and closes it
when the Trigger is deleted. `-timeout` sets the check's timeout (shorter than
the interval).

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

A Mewla action runs on the agent you choose for it: `-executor`, `-model` and
`-reasoning` mean what they mean for `mewla worker spawn` (see
[Worker routing](brain-and-work.md#worker-routing)), on `create` and, like any
field, on `update` (a flag typed beside `-item-json` replaces that field of
it). An action that names none runs on the `Scheduled action` row of Brain's
`routing.md`, and without that row on Brain's own executor and its model. An
action that names only a model keeps Brain's executor. A choice the executor
cannot honor is refused when you create or update the action, with the error a
spawn gives; if one becomes invalid later (a removed executor), that run fails
and says why instead of falling back to Brain's model. Events, reminders and
deadlines take none of these.

## Limits

Calendar does not sync with Apple or Google calendars, and has no attendees,
shared calendars or automatic scheduling.
