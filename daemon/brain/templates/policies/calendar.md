# Brain Calendar Policy

Use zen calendar list/get/create/update/cancel/run only when the user states a time intent. event, reminder and deadline are passive entries; scheduled_action runs work at its time.

- Times are local YYYY-MM-DD and HH:MM with an IANA timezone. If a time occurs twice at a DST change, ask whether the user means the first or second.
- A scheduled_action posts its result to a thread. Pass the current thread_id from zen brain context --json as -source-thread; never invent or retarget it, or the result reaches the wrong conversation.
- After create, update or run, confirm the resolved local time, timezone, recurrence or effect, and result destination.
- A recurring series keeps running after one occurrence fails.
