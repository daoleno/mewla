# Calendar

Calendar is Mewla's time view. Brain owns intent and lifecycle, Calendar owns when a commitment happens, Work owns execution, and Stats owns history.

## Model

The daemon is the source of truth. It stores a private SQLite database at `~/.mewla/calendar/calendar.db` (one row per item, one per run; see [the state store](state-store.md)); mobile clients sync snapshots over the authenticated WebSocket. Each item has one explicit kind:

- `event`: `start_at` and `end_at`
- `reminder`: `notify_at`
- `deadline`: `due_at`
- `scheduled_action`: `due_at`, `action_instruction`, and the originating Brain conversation in `source_thread_id`; optionally `summary`, one line (at most 120 characters) in the user's language that Calendar shows instead of the instruction

The user-facing app and CLI accept a local date, local wall time, and IANA timezone; RFC 3339 instants are only the internal wire/storage representation. DST gaps are rejected explicitly. When a wall time occurs twice during fall-back, the user must choose the first or second occurrence. Recurrence (`none`, `daily`, `weekly`, or `weekdays`) advances in that timezone using calendar dates, preserving the intended local wall clock through daylight-saving changes.

States are `scheduled`, `waiting`, `running`, `completed`, `failed`, and `cancelled`. Reminder and deadline items become waiting when due. Events run during their interval and complete afterward. A scheduled action is atomically claimed before any agent is launched.

## Scheduled actions

A scheduled action creates a visible `calendar_action` Work item and launches it through the configured executor in a dedicated, non-delegated Session. Launching Work only makes the Calendar item `running`; it is not completion. The scheduler reconciles the linked Work frontmatter and Worker lifecycle and records completion or failure only after a terminal signal. The executor session never enters generic agent-lifecycle OS notifications; Calendar owns the terminal result alert.

The generated Work file has a deterministic `User-facing deliverable` section delimited by exactly one ordered pair of `mewla:scheduled-deliverable` markers. The executor must replace the placeholder inside those markers with the complete result intended for the user, preserving useful paragraphs, lists, links, and citations while excluding instructions, progress notes, and Calendar metadata. Mewla accepts that section only from the freshly read, correctly linked terminal Work file, rejects malformed or larger-than-256-KiB content, and never substitutes Work digest fields or an executor summary.

Every `scheduled_action` requires `source_thread_id`. Claiming an occurrence freezes its title and captured Brain thread in the new Run before Work launches. A terminal Run is the sole durable owner of the extracted result or concise failure, and `FinishRun` is the one terminal commit. Server snapshots and Brain-thread timeline events derive the deterministic identity `calendar_result:<item-id>:<run-id>` and presentation directly from that Run; no second Brain transcript or delivery record is written. A captured thread that is not in the Brain registry is rejected before Work launches.

Recurring actions advance to their next wall-clock occurrence after either success or failure; the failed run remains in history and does not disable the series. Editing the recurring item later cannot change a completed Run's title or destination thread. Rebuilt result projections count toward per-thread Brain unread state until that thread is viewed. Immediately after the first successful terminal Run commit, the daemon makes one best-effort remote-push attempt that deep-links to the captured Brain thread; it never includes the result or failure contents in the alert. There is no outbox or restart replay, so missing registration, delivery failure, an in-process event drop, or a crash after persistence may produce no OS alert without changing the durable Run. If the captured thread is no longer the live Host thread, the app displays it explicitly read-only so its composer cannot send to a different conversation.

The durable claim is the idempotency boundary. After restart, Mewla reconciles a running claim and never launches the same occurrence again. If its linked Work and agent are no longer observable, the run fails with an uncertain-outcome explanation so the user can inspect and explicitly choose Run now.

A failure belongs to its run. The item keeps `failure_reason` only while its own status is `failed` (a one-off action, or a running item with no durable run record); a series that goes on is `scheduled`. Items stored before this rule can still carry a run's failure while scheduled; lists, gets and the CLI drop it (`calendar.WithoutStaleFailure`).

Lists and `calendar_item_changed` broadcasts carry each item's newest 10 runs, each with `result_preview` (its first readable line) in place of `result`; `get_calendar_item` returns every run in full. Brain's `current_work` and `open_work` entries for a Calendar occurrence carry `calendar: {item_id, run_id}`, and the occurrence's Work file carries `calendar_item_id` and `calendar_run_id`, so the app can name the schedule wherever that Work or its Session appears.

The scheduler does not poll. It sleeps on one timer armed for the earliest instant any item is due (an item's time, an event's end, an owner's retry) and looks again when the Calendar store changes, an owner registers, the linked Work file changes, or a Worker changes state. A one-minute safety net re-checks everything in case the clock jumped or the machine slept. A failed store write (a status change, a terminal Run) is logged once per distinct cause and retried on the next pass.

At startup, a scheduled action missed by no more than 15 minutes runs as catch-up. Older scheduled actions fail visibly instead of unexpectedly starting stale work. Overdue reminders and deadlines remain waiting and visible.

## Brain and API control

The mobile API supports list, get, create, update with revision checking, cancel, and Run now. The same operations are deterministic local control-plane commands:

```text
mewla calendar list --json
mewla calendar get -id <item-id> --json
mewla calendar create -title "Review plan" -kind reminder \
  -date 2026-07-15 -time 09:30 -timezone Asia/Shanghai --json
mewla calendar create -title "Summarize AI papers" -kind scheduled_action \
  -date 2026-07-15 -time 09:30 -timezone Asia/Shanghai \
  -instruction "Summarize the three linked papers" \
  -source-thread <brain-thread-id> --json
mewla calendar update -item-json '<complete item JSON>' -revision <revision> --json
mewla calendar cancel -id <item-id> -revision <revision> --json
mewla calendar run -id <item-id> --json
```

Create, update, cancel, get, and run responses include a plain confirmation with the resolved local date, time, timezone, and effect. In the CLI, `-source-thread` supplies `source_thread_id` and is mandatory for `scheduled_action`; callers updating with `-item-json` must preserve the same field. Brain is instructed to use these tools only for explicit calendar intent, not to extract commitments from every message.

## Alerts

The scheduler fires one alert per event, reminder or deadline occurrence (`Item.AlertAt`: its snooze, or its time less the lead; default lead event 10 min, deadline 30, reminder 0; `alert_lead_minutes` overrides). Owned items and scheduled actions never alert.

- The fired alert is recorded before it is sent (`alerted_at`, a bumped revision), so a reload or restart never sends it twice. `acked_at` is the last alert the user answered; an alert is open while `alerted_at` is later. Only the daemon sets both and `snoozed_until`; `Store.Update` keeps them and answers the open alert when the time moves.
- An alert more than `AlertWindow` (15 min) late is dropped, which is also what keeps a first run on an old calendar from sending a burst. A failed send is retried three times a minute apart inside that window; no registered phone is not retried.
- `snooze_calendar_item` / `done_calendar_item` (and `mewla calendar snooze|done`) answer it. Done finishes a reminder or deadline (a repeating one that already moved on only answers the alert) and only silences an event.
- The push is `push.KindCalendarAlert`, so Watch records it like any notification. Android devices (they say `platform: android` in `register_push`) get a data-only message their app posts and dedups against its own alarm; iOS gets a visible one, `time-sensitive` unless the device sent `strong_alerts: false`.

## Device alerts

The app arms the next alert of every item on the phone, so it fires with the daemon or the network late. Android hands the plan (`alertPlan`, `client/core/calendar/alerts.ts`) to the host (`CalendarAlerts` in `client/android`) (`AlarmManager.setExactAndAllowWhileIdle`, re-armed after boot, deduped with the push by `alert_key` under one lock); iOS schedules a `UNCalendarNotificationTrigger` (`client/ios/Sources/Calendar/CalendarAlerts.swift`). The in-app card (`client/ui/calendar/AlertCard.tsx`) is derived from the synced items and the phone's clock (`client/ui/calendar/alerts.ts`), not from how an alert arrived. Permission denial is non-fatal: Calendar remains usable and explains that notifications are disabled.

## Triggers

`daemon/trigger` owns Triggers: a check command on an interval that wakes Brain when its output changes. It is its own store (`<state-dir>/triggers/triggers.db`, one `trg_item` row of JSON per Trigger; payload files under `<state-dir>/triggers/<id>/`), not a Calendar kind: Calendar items are wall-clock occurrences that each launch an agent, while a Trigger's checks are not occurrences and almost all do nothing.

- **Scheduler.** One goroutine sleeps on a timer for the earliest due check (`last_checked_at + interval`, or `resumed_at`), wakes on store changes, and has a one-minute safety net. At most one check per Trigger and two across all Triggers run at once. On start each Trigger is checked once when due; a check more than three intervals late records `gap_from`/`gap_to` and fires nothing.
- **Check.** `/bin/sh -c` on Unix; on Windows `pwsh` when on PATH, else `powershell.exe` (`-NoProfile -NonInteractive -Command`). The environment is the daemon's with PATH from `hostdeps.UserPATH` (login-shell PATH, then the daemon's, then installer and per-user Node directories that exist), `TERM=dumb` and `MEWLA_TRIGGER_ID`. No stdin, own process group killed at the timeout, 64 KiB of stdout, the last 4 KiB of stderr.
- **Idle costs nothing.** A check whose trimmed output is empty or hashes like the previous successful check's only updates the Trigger row. No Work event, no Work revision, no runtime problem.
- **Firing.** A changed output becomes a firing (`fires/<seq>.txt`) raised with `brain.Service.RaiseWorkEvent`: the event is appended and its lifecycle Review opened (`OpenReviewEvent`), the same path machine resource pressure uses. A plain actionable `AppendWorkEvent` opens no Review, and the projection clears its `actionable`, so it wakes nobody; `mewla brain work event -actionable` therefore goes through `DeliverWorkEvent`, which opens the Review or fails with `brain_work_event_not_delivered` and the reason. The Trigger is the producer its Work waits on, so it appends with `AppendProducerWorkEvent`: a parked typed wake (Brain's `-wait-for`) does not demote the event to history the way a generic append does, and opening its Review consumes the wake; a Work a Worker's turn owns still takes it as history. When the Review cannot open (another one is still with Brain, or a Worker owns the Work), the firing is held and retried on the next check under a new dedupe key (`trigger:<id>:fire:<seq>:try:<n>`); changes meanwhile append to the held payload. While a delivered firing is not handled, changed outputs gather in `pending` and become the next firing once it is. A firing counts as handled when its Review was delivered and Brain's handling ended, or was resolved or replaced (`WorkEventHandled`); one undelivered for `NotHandledAfter` (3 h) gives the Trigger `attention: not_handled`.
- **Notices.** `trigger.failing` (3 failed checks spanning 30 min; again daily) and `trigger.paused` (the 7th wake within an hour pauses the Trigger, `pause_reason: runaway`) are actionable, held and retried like a firing; `trigger.recovered` is a record.
- **Work.** A Trigger creates a bounded Work in its source thread (closed when the Trigger is deleted) or adopts one (`-work`). `brain.Service.SetStandingWork` keeps every Trigger's Work out of the closure judge.
- **Retention.** The newest 50 firings and their files; deleting a Trigger deletes its directory.
- **App.** On connect the daemon sends `triggers_snapshot {triggers}`; `list_triggers` asks again. Every change broadcasts `trigger_changed {trigger}`, a deletion `trigger_removed {trigger_id}`. The wire view (`server/triggers.go`) carries the newest 10 firings and `pending_changes`, never the pending outputs, wake times or output hash. `pause_trigger`, `resume_trigger` and `check_trigger` (`{id}`) answer `trigger_updated {trigger}` (a check answers when it finishes, up to its timeout); `delete_trigger` answers `trigger_removed` and closes an owned Work as the CLI does. Errors: `trigger_not_found`, `invalid_trigger`, `already_running`. `attention` is `runaway`, else `failing` unless the person paused it, else `not_handled` (a lost firing, or one open for 3 h). The app (`client/core/triggers`) works out each row's words and `attention` the same way from the phone's clock, so "not handled" appears after 3 h without a new message; `triggersNeedingYou` feeds the tray's Needs you, and the tray leaves a Trigger's Work (`work_id`) out unless Brain is running it or it asks the person.

V1 does not sync Apple or Google calendars and does not include attendees, shared calendars, meeting rooms, complex colors, or arbitrary automatic scheduling.
