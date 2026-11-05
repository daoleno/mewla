# Brain Calendar Policy

Use mewla calendar list/get/create/update/cancel/run only when the user states a time intent. event, reminder and deadline are passive entries; scheduled_action runs work at its time.

- Times are local YYYY-MM-DD and HH:MM with an IANA timezone. If a time occurs twice at a DST change, ask whether the user means the first or second.
- The phone alerts before an event (10 min by default) or deadline (30 min) and at a reminder's time. When the user asks to be told earlier or later (「提前半小时提醒我」), pass -lead <minutes> on create, or set alert_lead_minutes in the -item-json of an update. Do not move an event's time to change the alert. For a reminder, set the reminder's own time.
- mewla calendar snooze -id <id> -minutes N and mewla calendar done -id <id> answer an alert for the user, only when they ask.
- Give every scheduled_action a -summary: one line in the user's language saying what it does and for whom, e.g. 「每晚从当天的工作里挖出可发的稿子」. Calendar shows it; -instruction is the full prompt the Worker gets. When you update an item that has none, add one.
- A scheduled_action posts its result to a thread. Pass the current thread_id from mewla brain context --json as -source-thread; never invent or retarget it, or the result reaches the wrong conversation.
- A scheduled_action runs on the user's default scheduled-action Worker (scheduled-action-route.toml), never on the Brain host. Pass -executor (with -model and -reasoning) on create only when the user explicitly asked for a different executor for that task, for example Codex; the item keeps that route. Never pass it to record your own preference, and do not repeat the default into the instruction. If a create is refused for having no Worker route, tell the user the default is not set; do not choose one yourself.
- After create, update or run, confirm the resolved local time, timezone, recurrence or effect, and result destination.
- A recurring series keeps running after one occurrence fails.

## Recurring and conditional commitments belong to Mewla

A commitment that must keep running after you are replaced (the user switches your executor, the daemon restarts, this session ends) is Mewla's, never your provider's. Do not use Claude /loop, CronCreate, CronList, ScheduleWakeup, a Codex timer, a background `until`/`sleep` loop or any timer inside your own session for it: those die with this session or this executor, silently.

- Fixed time or fixed recurrence of a self-contained job: mewla calendar create -kind scheduled_action (above).
- A goal you check repeatedly and finish when it is met, where you decide each time when to look again: mewla brain loop start -title -objective -done-criteria and one of -every (default gap) or -first-in/-first-at. Pass -executor/-model/-reasoning only if the user asked for a Worker other than the default; Mewla fixes the user's configured default Worker route on the loop when it starts, and a loop delegates with `worker spawn -work <loop>` on that route whichever executor you run on. Only start a loop the user authorized; the objective is theirs, not yours.
- Each check arrives as a Work event for the current Brain. Do the bounded check, then resolve it: mewla brain work resolve -disposition wait -wake-kind due_retry (with -next-attempt-at, or none to use the loop's gap) with a -summary of what you found (that summary is the loop's progress), -disposition complete when the goal is met, or -disposition cancel. Never re-arm by any other means.
- Inspect with mewla brain loop status; pause or resume with mewla brain loop pause/resume (resume also reschedules). Cancel or finish out of band with mewla brain work close. A paused, cancelled or finished loop never fires. Pausing while a check runs is allowed: that check finishes, but its resolution cannot re-arm the loop.
- Time spent paused or offline is not replayed: at most one check is pending, and you decide whether an external side effect should run again. Do not repeat an external send, purchase or deploy just because a check fired late.
- A timer left in your own provider's session (for example from before an executor switch) is not imported or recreated automatically. Before recreating one, check that the user's authorization is still active: a documented standing authorization (a worklog note or user instruction) stays valid, but verify it was not cancelled or superseded and that it was not already moved to a Calendar item or a loop, so nothing runs twice. List a timer whose origin you cannot establish as unknown; do not rebuild it.
