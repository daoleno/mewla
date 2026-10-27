# Brain Calendar Policy

Use mewla calendar list/get/create/update/cancel/run only when the user states a time intent. event, reminder and deadline are passive entries; scheduled_action runs work at its time.

- Times are local YYYY-MM-DD and HH:MM with an IANA timezone. If a time occurs twice at a DST change, ask whether the user means the first or second.
- Give every scheduled_action a -summary: one line in the user's language saying what it does and for whom, e.g. 「每晚从当天的工作里挖出可发的稿子」. Calendar shows it; -instruction is the full prompt the Worker gets. When you update an item that has none, add one.
- A scheduled_action posts its result to a thread. Pass the current thread_id from mewla brain context --json as -source-thread; never invent or retarget it, or the result reaches the wrong conversation.
- After create, update or run, confirm the resolved local time, timezone, recurrence or effect, and result destination.
- A recurring series keeps running after one occurrence fails.
