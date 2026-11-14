# Requests and Session state on the wire

What a device reads about a Session's requests and its running state. The user
guide is [Sessions](../sessions.md); this is the contract behind it.

## A permission prompt's subject

A Claude permission prompt also says what it asks about as data, not as screen
text: `tool` (`Bash`) and `summary` (the command, or the file), taken from the
tool call Claude wrote to its transcript before it asked. Claude's own hints in
the box (such as "Tip: auto mode handles these prompts") are left out of
`request`. They are absent when the transcript has no such call yet.

## Answered requests

**An answered request stays answered.** The daemon keeps one record for each
request that is answered: a screen prompt, an agent approval, an agent's
question, or Brain's question on a Work. The record holds the short form of
what was asked, the answer, the outcome (`answered`, `approved`, `denied`,
`dismissed`, or `not_answered` when it timed out), who answered, and when.
A permission or approval also carries `tool` and `summary`, the tool and the
command or file it asked about, so the sealed line can say what was approved.
One answered in the Session's own terminal gets its outcome (`approved` or
`denied`) from the tool call Claude recorded; the option picked there is not
recorded, so it has no `answer`.
A request from a delegated Worker also names the Work it was working on
(`work_id`). A Session that asks the same thing again gets a new record for
each time it asks. Who answered is one of:

- a paired device, with its id and name;
- Brain, with the sentence of its brief it relied on;
- an operator using `mewla worker answer`;
- the Session's own terminal, when the prompt left the screen and no answer
  came through Mewla within 5 seconds;
- a timeout.

The records are kept in `<state>/answered_requests.jsonl`: each Session's
newest 240 (its thread's window), and every Work question. A device can read
them in three places:

- In the request's thread, as a row of kind `request_answered` placed where
  the request was asked. The row carries `request`, whose `anchor` names the
  transcript call, screen prompt or approval when there is one. Brain's thread
  shows only what was answered in it (`thread_id`): Brain's own prompts, and
  questions on the Work that thread started. An app that does not know the
  kind drops the row.
- In the `request_answered` broadcast, sent to every device the moment a record
  is written.
- In `answered_requests`, the history the tray lists, newest first. It takes
  an optional `session_id` and a `limit`.

The first explicit answer stands. A terminal answer, which is inferred, gives
way to the app's or Brain's answer if that is recorded a moment later.

## Running state

**A Session that is working.** A Claude, Codex or Cursor Session you started
reports `running` while its own terminal says it works (Claude's footer shows
"esc to interrupt") and `unknown`, which apps list as idle, when it does not.
`running_since` is the poll that first saw the run. A prompt ends the run and
the answer starts a new one, so the time a Session waits on you is not counted.
A Claude Session that Mewla's hooks follow also carries `activity`, the tool
call it started last (`Bash curl -sI https://example.org`), with its parts in
`activity_tool` (`Bash`) and `activity_summary` (`curl -sI https://example.org`),
from the hook, not the screen. A Session whose agent is not followed by hooks
(one launched with its own `--settings`, for example) has `running_since` and
no `activity`. A Brain-delegated Worker keeps the status of its turn; its
`running_since` is when its turn was accepted, and after a prompt, the answer.
