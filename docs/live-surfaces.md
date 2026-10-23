# Live surfaces

A running task and a question that needs you can stay on the phone as one
ongoing notification and as home-screen widgets. The daemon decides what they
show; the app draws it. Nothing here is new state: it is read from the same
Brain snapshot the app already receives.

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
  "question": null
}
```

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
- A device gets live pushes only if its `register_push` said
  `"live_state": true`. Registering again without it turns them off, so a
  device that cannot show them (iOS, for now) receives nothing.

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

- One native module, `app/modules/mewla-live`, owns the surface. Its own FCM
  service takes the live data message with the app closed and hands every other
  message to expo-notifications unchanged.
- The notification is one ongoing notification (id 7101) that asks to be
  promoted: a status-bar chip (the seal icon and the elapsed time, or the count
  of open questions), a pinned card, and a segmented bar from `done` / `total`.
  With "Live updates" off for the app, or before Android 16 QPR1, the same
  content is a plain ongoing notification. It is taken down when the state goes
  idle, and a swiped-away run stays away until a new run key arrives.
- A chip or widget button opens the app on that question
  (`mewla://live/answer?q=…&c=…`), which sends the answer once.
- The widgets (2x2 and 4x2) draw from the last snapshot and repaint when a state
  arrives, by push or by socket. A token the daemon refuses clears the widgets
  and is not re-issued until the server is switched; an unreadable token is
  dropped and issued again.
- A server paired through Mewla Link has no address a widget can fetch, so its
  widgets are fed instead: every state the app applies, by socket or by a push
  that wakes the service with the app closed, is cached and repainted. States
  are ordered by `(epoch, seq)`, so a late one never overwrites a newer one.
  Nothing is issued for the widget on that path.

## Limits

- On a plain URL the widget fetches the daemon itself and a revoked phone stops
  at the next refresh. On Mewla Link the widget only shows what was pushed: it
  shows "Open Mewla" once its last state is more than 12 hours old. Revoking
  that phone stops the pushes (the daemon turns live pushes off for the device
  that registered them), but the widget keeps its last state until that point.
- Live pushes are not receipt-tracked: a newer state always follows, and a lost
  one costs nothing. Dead tokens and missing credentials are handled like any
  other push.
- A Work question is answered only if it is still the current one; a late tap
  changes nothing.
