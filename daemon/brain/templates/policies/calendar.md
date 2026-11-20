# Brain Calendar Policy

Use mewla calendar list/get/create/update/cancel/run only when the user states a time intent. event, reminder and deadline are passive entries; scheduled_action runs work at its time.

- Times are local YYYY-MM-DD and HH:MM with an IANA timezone. If a time occurs twice at a DST change, ask whether the user means the first or second.
- The phone alerts before an event (10 min by default) or deadline (30 min) and at a reminder's time. When the user asks to be told earlier or later (「提前半小时提醒我」), pass -lead <minutes> on create, or set alert_lead_minutes in the -item-json of an update. Do not move an event's time to change the alert. For a reminder, set the reminder's own time.
- mewla calendar snooze -id <id> -minutes N and mewla calendar done -id <id> answer an alert for the user, only when they ask.
- Give every scheduled_action a -summary: one line in the user's language saying what it does and for whom, e.g. 「每晚从当天的工作里挖出可发的稿子」. Calendar shows it; -instruction is the full prompt the Worker gets. When you update an item that has none, add one.
- For a scheduled_action, pass -executor, -model and -reasoning from the "Scheduled action" row of routing.md (or the row that fits the task); without them it runs on the row, else on your own executor and model.
- A scheduled_action posts its result to a thread. Pass the current thread_id from mewla brain context --json as -source-thread; never invent or retarget it, or the result reaches the wrong conversation.
- After create, update or run, confirm the resolved local time, timezone, recurrence or effect, and result destination.
- A recurring series keeps running after one occurrence fails.

## Triggers

When the user asks to watch something and act when it changes (「盯着 X，有变化就…」), create a Trigger, not a recurring scheduled_action and never a background shell loop: mewla trigger create -title "<what it watches>" -every 10m -check '<command>' -instruction '<what to do when it fires>' -source-thread <thread_id>.

- The check prints nothing when nothing happened and prints the evidence when something did; Mewla runs it without a model and wakes you only when its output differs from the previous check. Make repeated reminders print one stable line per period, not a changing count.
- A firing is a trigger.fired event on the Trigger's Work with the output in payload_ref; handle it like any Work event. Spawn Workers for it on their own Work, never with -work on the Trigger's Work: a Work a Worker owns cannot wake you. Resolve it with continue, never complete: completing closes the Trigger's Work. trigger.failing and trigger.paused mean the watching itself stopped: tell the user and fix or pause it.
- mewla trigger list shows each Trigger's state; pause, resume, check and delete take -id. Confirm what it watches, how often and what happens when it fires.
