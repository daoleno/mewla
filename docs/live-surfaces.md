# Live surfaces

What needs you and what is running stay on the phone outside the app: a live
update (Android's ongoing notification and status-bar chip; the Dynamic Island
and Lock Screen on iOS) and home-screen widgets. Each has one job:

- **The live update** is there only while something runs or needs you. It shows
  the one thing that matters now (what needs you, newest first, else what
  started last) with its agent's logo and progress ring, and one tap opens it.
- **The widgets** are the tray at a glance: the small one is that one thing, the
  medium one the header's words and three rows, each opening its Session or
  Work. With nothing to show, the cat naps.

The daemon decides what they show; the app draws it. Nothing here is new state:
it is read from the same Brain snapshot and Sessions the app already receives,
and counted the way the app's header counts.

## live_state

One projection, `live.State`, from the Brain snapshot:

```json
{
  "kind": "running",
  "epoch": 1792751234567,
  "seq": 12,
  "run_key": "run:%123@1792751300000",
  "title": "perpetuo-fre17-18",
  "phase": "releasing",
  "started_at": 1792751300000,
  "done": 2,
  "total": 5,
  "workers": [{"name": "main:7", "phase": "releasing"}],
  "worker_count": 1,
  "needs_count": 0,
  "question": null,
  "items": [
    {"key": "work:w1", "open": "session", "id": "%7", "title": "perpetuo-fre17-18",
     "line": "Releasing", "agent": "claude", "ring": {"stage": "moving", "percent": 45},
     "started_at": 1792751300000}
  ],
  "running": 1,
  "queued": 0,
  "waiting": 0
}
```

- `items` are what the surfaces draw, at most three: what needs the person,
  newest first, then what runs, the one that started last first. Each has where
  a tap goes (`open`: `work`, `session` or `brain`, and its `id`), its title, a
  line (what it does, or what it asks), the Session's `agent` (`brain` for
  Brain's mark: Brain itself, or a Work with several Workers), `needs`
  (`answer` or `approve`) while it waits for the person, the Work's progress
  `ring` as the app draws it (`moving` or `back` rounded to 5 and at most 95, or
  `last`; never a number for the person to read) and `started_at` for its clock.
- `running`, `queued` and `waiting` are the header's counts: a Work with several
  Workers is one row, and a Session the person started counts too.
  `needs_count` is the open-request count other computers show ("1 needs you"):
  Work questions, a Session's approvals or prompt (a delegated Worker's own
  report is Brain's, not the person's) and Brain's own prompt.

- `kind` is `idle`, `running` or `needs_you`. A question outranks running work.
- **Running** is every visible, delegated Worker the watcher classifies as
  running. One Worker shows its Work's title and phase; several collapse to
  "N workers". `started_at` is the earliest start (the elapsed clock).
- **Needs you** comes from a Work's own question (structured, with choices) or,
  failing that, the Brain chat chips. `question` is
  `{"id", "kind": "work"|"chat", "work_id", "text", "choices"}`, at most three
  choices, shortened for a notification. `needs_count` counts every open ask.
- `done` / `total` come from the current objective (`total` is 0 without one).
  `Objective.Done` is exposed here and nowhere else on the wire.
- `run_key` names the run: a new Worker starting, or a new question, is a new
  key. The phone uses it for "dismissed stays away until a new Work starts".
- The payload stays well under 2 KB.

`(epoch, seq)` orders states. The epoch is the daemon's boot time, so a restart
cannot make an old state look newer. Only changes a person would see produce a
new state; the elapsed clock and bookkeeping do not. A burst of changes is
coalesced for about 1.5 seconds, and a 20-second safety net catches changes no
broadcast announced.

## How it reaches the phone

- **WebSocket**: `{"type": "live_state", "live": <State>}` to every
  authenticated client on each change. `live_state_get` returns the current
  state (same shape, with the request's `request_id`) so a reconnecting phone
  catches up.
- **Push** (Android, while the app is not connected): a data-only, high-priority
  message through the existing Expo push path, with no title, body, sound or
  `channelId`. Expo turns a `channelId` into an FCM notification block, and a
  phone whose app is not running would then show an empty system notification
  instead of waking the app's service.
  Its data is strings: `mewla_live` = `"1"`, `server_ref` (the phone drops
  state from a server that is not its current one), `epoch`, `seq`, and
  `payload` (the State as JSON). Going idle after idle is not pushed.
- **Push** (iOS, while the app is not running): the same data message with
  `contentAvailable: true`, a silent push that wakes the app so it can update the
  Live Activity and widgets. This is the chosen route, not a fallback: the APNs
  key stays in Expo, and Expo's push API has no ActivityKit (`liveactivity`)
  push type, so the daemon sends the silent push and the app applies it. iOS
  does not deliver it to an app the user force-quit and throttles it, so an
  update can be late or, after a force-quit, missing until the app is opened
  again.
- A device gets live pushes only if its `register_push` said
  `"live_state": true`. Registering again without it turns them off, so a
  device that cannot show them (an iOS build without the extension) receives
  nothing.

## Answering from the notification

`live_answer` with `{request_id, question_id, choice_index}` (0-based):

- The daemon recomputes the current question and requires `question_id` to
  match it. Otherwise it answers `answer_stale` and sends nothing.
- A **Work** question is answered through the Work's existing reply
  (`brain_work_action`'s reply), with the full choice text and the request id.
- A **chat** chip has no daemon-side send, so the daemon only vouches for it
  (`kind: "chat"`, `text`) and the app sends the text through its own outbox.
- The same `request_id` twice is the same answer: the second reply carries
  `"duplicate": true` and nothing is submitted again.

## Widget snapshot token

A home-screen widget cannot hold the device's signing key and must not be able
to do anything but read. It gets its own credential:

- `widget_token_issue` (an authenticated device) returns
  `{"token", "id", "url_path": "/widget/snapshot"}` once. The daemon keeps only
  the token's hash, in `widget_tokens.json` (0600) in its state directory.
- One token per device: issuing again replaces the earlier one.
- `widget_token_revoke` removes the caller's token. Revoking the device removes
  its token, and a token whose device is no longer trusted is refused.
- `GET /widget/snapshot` with `Authorization: Bearer <token>` returns
  `{"live": <State>, "generated_at": <ms>}` with `Cache-Control: no-store`.
  Unknown, revoked or missing tokens get `401` with `WWW-Authenticate: Bearer`;
  other methods get `405`. Pairing and device credentials are not accepted
  there, and the token opens no other route.

A push only tells the widget "changed, fetch now"; the widget reads the state
from this endpoint, so a revoked token stops it for good.

## On the phone (Android)

- The Android host (`client/android`, package `live`, with `LiveController`,
  `LiveNotifier` and `WidgetSync`) owns the surface. Its FCM service
  (`MewlaFirebaseMessagingService`) takes the live data message with the app
  closed; a calendar alert push goes to the calendar alert code, and any other
  message is drawn as a notification.
- The live update is one ongoing notification (id 7101) that asks to be
  promoted. It shows the first item: its title, what it does or asks, the
  header's words ("3 running", "1 needs you") and, as the large icon, its
  agent's logo with the progress ring or the red dot. The status-bar chip
  (ColorOS's 流体云 capsule) is the system's: Mewla's mark and the item's clock
  while it runs, or its tag ("Answer", "Approve") while it waits for you.
  Promotion allows no custom views and no colour of our own, so the ring is a
  drawn bitmap. With "Live updates" off for the app, or before Android 16 QPR1,
  the same content is a plain ongoing notification. It is taken down when the
  state goes idle, and a swiped-away run stays away until a new run key arrives.
- A tap opens that item (`mewla://sessions/<id>`, `mewla://brain/work/<id>`,
  `mewla://brain`, naming the computer). A Work's question carries its choices;
  a choice opens the app on that question (`mewla://live/answer?q=…&c=…`), which
  sends the answer once. A Session's approval or prompt opens its card.
- The widgets (2x2 and 4x2) draw from the last state and repaint when one
  arrives, by push or by socket. The small one shows the first item (its logo
  and ring, title, and what it asks or "+2 running"); the medium one the header's
  words and up to three rows, each with its logo, line and live clock or red
  tag, and each opening its Session or Work. Quiet, the cat naps over "All
  quiet" or "1 queued"; a widget that knows nothing (not set up, refused, or
  nothing heard for 12 hours) shows the cat greyed, with no words.
- Debug builds have `live.LivePreviewActivity`, which draws every surface from
  sample states (`--es post running|needs` posts the live update too). A token the daemon refuses clears the widgets
  and is not re-issued until the server is switched; an unreadable token is
  dropped and issued again.
- A server paired through Mewla Relay has no address a widget can fetch, so its
  widgets are fed instead: every state the app applies, by socket or by a push
  that wakes the service with the app closed, is cached and repainted. States
  are ordered by `(epoch, seq)`, so a late one never overwrites a newer one.
  Nothing is issued for the widget on that path.

## On the phone (iOS)

- The iOS host (`client/ios/Sources/Live`) and a WidgetKit extension,
  `MewlaLiveWidgets` (`client/ios/LiveExtension`, a target of
  `client/ios/project.yml`). The app and the extension compile the same
  `MewlaLiveState.swift`, so the state has one shape.
- The Live Activity shows the first item, as Android's live update does: on the
  Lock Screen and in the expanded Dynamic Island its logo with the progress ring
  (or the red dot), its title and what it does or asks, its clock, and either a
  Work question's choices or the next two items; compact, the logo and the clock
  or its tag ("Answer", "Approve"); minimal, the logo. A tap opens the item; a
  choice opens `mewla://live/answer?q=…&c=…`, which sends the answer once. It
  starts while the app is in the foreground (iOS only allows that), updates in
  the background, and ends when nothing runs or waits. A removed card stays away
  until a new run key arrives.
- Widgets read `live_state.json` from the App Group the app writes on every
  state, and, on a plain URL, fetch `/widget/snapshot` with the same revocable
  token as Android. Small is the first item, medium the tray (the header's words
  and three rows, each a link to its Session or Work); on the Lock Screen a round
  one shows the first item's ring and a line one the header's words and the
  item. Quiet, the cat naps; a refused token or nothing heard for 12 hours shows
  the cat greyed. Mewla Relay servers show only what the app and pushes applied.
- The app's own logos (`client/ui/icons/agents`, Brain's mark and the terminal
  glyph) and the cat's stills are vector and PNG sets in the extension's asset
  catalog.
- The extension is always built and embedded, so every iOS build needs its App ID,
  App Group and provisioning profile: see `docs/ios-ci-release.md`.

## Limits

- iOS: with the app closed, a Live Activity is updated by the silent push above
  (best effort: late sometimes, absent after a force-quit) and cannot be started
  from the background, so the first card of a run appears once the app is open.

- On a plain URL the widget fetches the daemon itself and a revoked phone stops
  at the next refresh. On Mewla Relay the widget only shows what was pushed: it
  shows the greyed cat once its last state is more than 12 hours old. Revoking
  that phone stops the pushes (the daemon turns live pushes off for the device
  that registered them), but the widget keeps its last state until that point.
- Live pushes are not receipt-tracked: a newer state always follows, and a lost
  one costs nothing. Dead tokens and missing credentials are handled like any
  other push.
- A Work question is answered only if it is still the current one; a late tap
  changes nothing.
